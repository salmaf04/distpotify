import { parseBlob } from 'music-metadata-browser';

type ExtractedMetadata = {
  title: string;
  album: string | null;
  genre: string | null;
  artist: string | null;
  duration: number | null;
  coverFile?: File | null;
};

export async function getDuration(file: File): Promise<number | null> {
  try {
    const metadata = await parseBlob(file as Blob);
    if (metadata.format && typeof metadata.format.duration === 'number') return metadata.format.duration;
  } catch {
    // fallthrough to HTMLAudioElement
  }
  return await getAudioDurationFromBlob(file);
}

async function blobToFile(blob: Blob, fileName: string, mimeType?: string): Promise<File> {
  return new File([blob], fileName, { type: mimeType || blob.type });
}

function normalizePictureData(data: unknown): ArrayBuffer {
  if (data instanceof ArrayBuffer) {
    const copy = new ArrayBuffer(data.byteLength);
    new Uint8Array(copy).set(new Uint8Array(data));
    return copy;
  }

  if (ArrayBuffer.isView(data)) {
    const view = data ;
    const copy = new ArrayBuffer(view.byteLength);
    new Uint8Array(copy).set(new Uint8Array(view.buffer, view.byteOffset, view.byteLength));
    return copy;
  }

  if (typeof data === 'object' && data !== null) {
    try {
      const arr = Array.from(data as Iterable<number>);
      const copy = new ArrayBuffer(arr.length);
      new Uint8Array(copy).set(new Uint8Array(arr));
      return copy;
    } catch {
      return new ArrayBuffer(0);
    }
  }

  return new ArrayBuffer(0);
}

async function extractCoverFromMetadata(metadata: unknown, title: string, originalName: string): Promise<File | null> {
  if (!metadata || typeof metadata !== 'object') return null;
  const md = metadata as { common?: { picture?: Array<{ data: unknown; format?: string }> } };
  if (!md.common || !Array.isArray(md.common.picture) || md.common.picture.length === 0) return null;
  const pic = md.common.picture[0];
  const arrayBuffer = normalizePictureData(pic.data);
  if (arrayBuffer.byteLength === 0) return null;
  const blob = new Blob([new Uint8Array(arrayBuffer)], { type: pic.format || 'image/jpeg' });
  const ext = pic.format?.split('/')?.[1] || 'jpg';
  try {
    return await blobToFile(blob, `${title || originalName}-cover.${ext}`, pic.format);
  } catch {
    return null;
  }
}

function parseCommonTags(
  metadata: unknown,
  cb: (title: string | null, album: string | null, genre: string | null, artist: string | null) => void
) {
  if (!metadata || typeof metadata !== 'object') return cb(null, null, null, null);
  const md = metadata as { common?: Record<string, unknown> };
  const common = md.common as Record<string, unknown> | undefined;
  if (!common) return cb(null, null, null, null);

  const getStringOrFirst = (v: unknown): string | null => {
    if (typeof v === 'string' && v.length > 0) return v;
    if (Array.isArray(v) && v.length > 0) return typeof v[0] === 'string' ? v[0] : String(v[0]);
    return null;
  };

  const title = getStringOrFirst(common.title);
  const album = getStringOrFirst(common.album);
  const genre = getStringOrFirst(common.genre ?? common.genres);
  // common may expose artist singular or artists array or performer
  const artist = getStringOrFirst(common.artist ?? (common.artists as unknown) ?? common.performer);

  cb(title, album, genre, artist);
}

function findInNative(metadata: unknown, ids: string[]): string | null {
  if (!metadata || typeof metadata !== 'object') return null;
  const md = metadata as { native?: Record<string, unknown> };
  const native = md.native as Record<string, unknown> | undefined;
  if (!native || typeof native !== 'object') return null;
  for (const key of Object.keys(native)) {
    const items = native[key];
    if (!Array.isArray(items)) continue;
    for (const item of items) {
      if (!item || typeof item !== 'object') continue;
      const itemRec = item as Record<string, unknown>;
      const idVal = itemRec['id'] ?? itemRec['tag'] ?? itemRec['ID'] ?? itemRec['key'];
      if (!idVal) continue;
      const idStr = String(idVal);
      if (!ids.includes(idStr)) continue;

      const value = itemRec['value'] ?? itemRec['data'] ?? itemRec['text'] ?? itemRec;
      if (Array.isArray(value) && (value as unknown[]).length > 0) return String((value as unknown[])[0]);
      return String(value as unknown);
    }
  }
  return null;
}

export async function extractMetadata(file: File): Promise<ExtractedMetadata> {
  // Fallback values
  let title = file.name.replace(/\.[^/.]+$/, '');
  let album: string | null = null;
  let genre: string | null = null;
  let artist: string | null = null;
  let duration: number | null = null;
  let coverFile: File | null | undefined = null;

  try {
    const metadata = await parseBlob(file as Blob);

    // parse common tags + cover
    parseCommonTags(metadata, (t, a, g, ar) => {
      title = t || title;
      album = a ?? album;
      genre = g ?? genre;
      artist = ar ?? artist;
    });
    // If some fields are missing, try native frames (ID3)
    if (!artist) {
      const found = findInNative(metadata, ["TPE1", "TPE2", "TPE3", "TOPE", "ARTIST", "ARTISTS"]);
      if (found) artist = found;
    }
    if (!album) {
      const found = findInNative(metadata, ["TALB", "ALBUM"]);
      if (found) album = found;
    }
    if (!genre) {
      const found = findInNative(metadata, ["TCON", "GENRE"]);
      if (found) genre = found;
    }
    coverFile = await extractCoverFromMetadata(metadata, title, file.name);

    // Format/duration
    if (metadata.format && typeof metadata.format.duration === 'number') {
      duration = metadata.format.duration;
    } else {
      // The library sometimes doesn't provide duration for some files - fallback using HTMLAudioElement
      duration = await getAudioDurationFromBlob(file);
    }
  } catch {
    // If metadata parsing fails, attempt to get duration as a best-effort
    try {
      duration = await getAudioDurationFromBlob(file);
    } catch {
      duration = null;
    }
  }

  return { title, album, genre, artist, duration, coverFile };
}

async function getAudioDurationFromBlob(file: File): Promise<number | null> {
  return new Promise((resolve) => {
    try {
      const url = URL.createObjectURL(file);
      const audio = new Audio();
      audio.preload = 'metadata';
      audio.src = url;
      const clear = () => {
        URL.revokeObjectURL(url);
      };
      audio.addEventListener('loadedmetadata', () => {
        const d = isFinite(audio.duration) ? audio.duration : null;
        clear();
        resolve(d);
      });
      audio.addEventListener('error', () => {
        clear();
        resolve(null);
      });
    } catch {
      resolve(null);
    }
  });
}

export async function buildUploadFormData(file: File, extraFields?: Record<string, string | Blob>): Promise<FormData> {
  const duration = await getDuration(file);

  const form = new FormData();
  if (duration !== null && duration !== undefined) form.append('duration', String(Math.round(duration)));

  form.append('audio', file, file.name);

  if (extraFields) {
    for (const [k, v] of Object.entries(extraFields)) {
      form.append(k, v as Blob | string);
    }
  }

  return form;
}

export default {
  extractMetadata,
  buildUploadFormData,
};

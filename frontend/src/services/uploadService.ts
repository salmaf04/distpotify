import type { Song } from "../types/song";
import type { UploadItem } from "../types/uploadSong";


type ProgressCallback = (percent: number) => void;

export type UploadResult = {
  success: boolean;
  data?: Song[];
  error?: string;
};

function parseDurationToSeconds(d?: string | number | null): number {
  if (!d && d !== 0) return 0;
  if (typeof d === 'number') return Math.round(d);
  if (typeof d === 'string') {
    const parts = d.split(':').map(Number).filter((n) => !Number.isNaN(n));
    if (parts.length === 1) return Math.round(parts[0]);
    if (parts.length === 2) return parts[0] * 60 + parts[1];
    if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2];
  }
  return 0;
}

function isFileList(input: FileList | UploadItem[] | null): input is FileList {
  return !!input && typeof (input as FileList).item === 'function';
}

function mapUploadItemToFormData(form: FormData, item: UploadItem) {
  // backend expects fields similar to SongInputModel (title, artist, album, genre, duration, file, cover)
  form.append('title', item.title ?? item.file.name.replace(/\.[^/.]+$/, ''));
  form.append('artist', item.artist ?? 'Artista Desconocido');
  form.append('album', item.album ?? '');
  form.append('genre', item.genre ?? '');
  if (item.duration !== undefined && item.duration !== null) form.append('duration', String(Math.round(item.duration)));
  // ensure filename extension is lowercase .mp3 to satisfy backend extension check
  const originalName = item.file.name;
  const extRe = /(\.[^/.]+)$/;
  const extExec = extRe.exec(originalName);
  const ext = extExec ? extExec[1].toLowerCase() : '';
  const safeName = ext ? originalName.replace(/(\.[^/.]+)$/, ext) : originalName;
  form.append('file', item.file, safeName);
  if (item.coverFile) form.append('cover', item.coverFile, item.coverFile.name);
}

async function uploadFiles(
  input: FileList | UploadItem[] | null,
  onProgress?: ProgressCallback,
  signal?: AbortSignal
): Promise<UploadResult> {
  if (!input) return { success: false, error: 'No files provided' };

  try {
    // If FileList, build form per file and send sequentially; if UploadItem[] we can send multiple using same form per file
    const items: UploadItem[] = isFileList(input)
      ? Array.from(input).map((f) => ({ file: f, title: f.name.replace(/\.[^/.]+$/, '') }))
      : (input as UploadItem[]);

    const uploadedSongs: Song[] = [];

    // upload files sequentially so we can report per-file progress simply
    for (const item of items) {
      const form = new FormData();
      mapUploadItemToFormData(form, item);

  const xhr = new XMLHttpRequest();
  const API_BASE = (import.meta.env.VITE_API_BASE as string) ?? '';
  const base = API_BASE.replace(/\/$/, '');
  const url = base ? `${base}/api/songs/upload` : '/api/songs/upload';

      const parseXhrResponse = (xhr: XMLHttpRequest) => {
        const status = xhr.status;
        const text = xhr.responseText;
        let parsed: unknown = null;
        try {
          if (text) parsed = JSON.parse(text);
        } catch {
          parsed = text || null;
        }
        return { status, parsed, text };
      };

      const prom = new Promise<UploadResult>((resolve, reject) => {
  xhr.open('POST', url, true);
        if (signal) {
          signal.addEventListener('abort', () => {
            xhr.abort();
            reject(new Error('Upload aborted'));
          });
        }

        xhr.onload = () => {
            const { status, parsed } = parseXhrResponse(xhr);
            if (status >= 200 && status < 300) {
              // prefer resp.data if present; backend returns created song (or array) with duration as string
              if (parsed && typeof parsed === 'object') {
                const asObj = parsed as Record<string, unknown>;
                const data = asObj.data ?? parsed;
                const getStr = (o: Record<string, unknown>, k: string): string | undefined => {
                  const v = o[k];
                  return typeof v === 'string' ? v : undefined;
                };

                const pushSong = (s: Record<string, unknown>) => {
                  const idVal = getStr(s, 'id') ?? String(getStr(s, 'ID') ?? `uploaded-${Date.now()}`);
                  const titleVal = getStr(s, 'title') ?? getStr(s, 'name') ?? '';
                  const artistVal = getStr(s, 'artist') ?? 'Artista Desconocido';
                  const albumVal = getStr(s, 'album') ?? 'Álbum Desconocido';
                  const genreVal = getStr(s, 'genre') ?? 'Desconocido';
                  const durationVal = getStr(s, 'duration') ?? undefined;
                  const fileVal = getStr(s, 'file');
                  const coverVal = getStr(s, 'cover');

                  const song: Song = {
                    id: String(idVal),
                    title: titleVal,
                    artist: artistVal,
                    album: albumVal,
                    genre: genreVal,
                    duration: parseDurationToSeconds(durationVal ?? 0),
                    audioUrl: fileVal,
                    coverUrl: coverVal,
                  };
                  uploadedSongs.push(song);
                };

                if (Array.isArray(data)) {
                  (data as Record<string, unknown>[]).forEach(pushSong);
                } else if (data && typeof data === 'object') {
                  pushSong(data as Record<string, unknown>);
                }
              }
              resolve({ success: true, data: uploadedSongs });
            } else {
              let msg = `Upload failed with status ${status}`;
              if (parsed) {
                msg = typeof parsed === 'string' ? parsed : JSON.stringify(parsed);
              }
              resolve({ success: false, error: msg });
            }
          };

        xhr.onerror = () => resolve({ success: false, error: 'Network error' });

        if (xhr.upload && typeof onProgress === 'function') {
          xhr.upload.onprogress = (ev) => {
            if (ev.lengthComputable) {
              const percent = Math.round((ev.loaded / ev.total) * 100);
              onProgress(percent);
            }
          };
        }

        // debug: log the file name that will be sent and the endpoint
        const maybeFile = form.get('file');
        if (maybeFile && typeof (maybeFile as File).name === 'string') {
          const f = maybeFile as File;
          console.debug('[uploadService] sending', { url, fileName: f.name });
        } else {
          console.debug('[uploadService] sending', { url, fileName: '(not available in FormData API)' });
        }

        xhr.send(form);
      });

      const result = await prom;
      if (!result.success) return result;
    }

    return { success: true, data: uploadedSongs };
  } catch (err: unknown) {
    let message = String(err);
    if (err && typeof err === 'object') {
      const e = err as Record<string, unknown>;
      if (typeof e.message === 'string') message = e.message;
    }
    return { success: false, error: message };
  }
}

export default {
  uploadFiles,
};

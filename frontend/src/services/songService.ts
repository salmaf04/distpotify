import type { Song } from "../types/song";
import type { SongResponse } from "../types/api";

function parseDurationToSeconds(d?: string): number {
  if (!d) return 0;
  // format mm:ss or h:mm:ss
  const parts = d.split(':').map(Number).filter((n) => !Number.isNaN(n));
  if (parts.length === 0) return 0;
  if (parts.length === 1) return Math.round(parts[0]);
  if (parts.length === 2) return parts[0] * 60 + parts[1];
  if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2];
  return 0;
}

async function fetchSongs(signal?: AbortSignal): Promise<Song[]> {
  const API_BASE = import.meta.env.VITE_API_URL
  const base = API_BASE.replace(/\/$/, '');
  const url = base ? `${base}/api/songs` : '/api/songs';

  const resp = await fetch(url, { method: 'GET', signal });
  if (!resp.ok) throw new Error(`Failed to fetch songs: ${resp.status}`);
  const data = await resp.json();
  if (!Array.isArray(data)) return [];

  const mapped: Song[] = (data as SongResponse[]).map((s) => {
    let file = s.file ?? undefined;
    let cover = s.cover ?? undefined;
    // backend returns raw base64 without data: prefix; if so, wrap with data URI
    if (typeof file === 'string' && file.length > 0 && !file.startsWith('data:')) {
      // assume audio/mpeg for mp3 stored by backend
      file = `data:audio/mpeg;base64,${file}`;
    }
    if (typeof cover === 'string' && cover.length > 0 && !cover.startsWith('data:')) {
      // attempt jpeg by default
      cover = `data:image/jpeg;base64,${cover}`;
    }
    return {
      id: String(s.id),
      title: s.title,
      artist: s.artist ?? 'Artista Desconocido',
      album: s.album ?? 'Álbum Desconocido',
      genre: s.genre ?? 'Desconocido',
      duration: parseDurationToSeconds(s.duration),
      audioUrl: typeof file === 'string' ? file : undefined,
      coverUrl: typeof cover === 'string' ? cover : undefined,
    } as Song;
  });

  return mapped;
}

export default { fetchSongs };

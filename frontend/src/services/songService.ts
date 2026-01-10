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

export async function fetchSongs(signal?: AbortSignal): Promise<Song[]> {
  const url = '/api/songs';

  const resp = await fetch(url, { method: 'GET', signal });
  if (!resp.ok) throw new Error(`Failed to fetch songs: ${resp.status}`);
  
  const data = await resp.json();
  if (!Array.isArray(data)) return [];

  const mapped: Song[] = (data as SongResponse[]).map((s) => {
    // Manejo de portada (Cover)
    let cover = s.cover ?? undefined;
    if (typeof cover === 'string' && cover.length > 0 && !cover.startsWith('data:') && !cover.startsWith('http')) {
      cover = `data:image/jpeg;base64,${cover}`;
    }

    // LÓGICA DE STREAMING:
    // Construimos la URL directa al endpoint de Fiber
    const streamUrl = `/api/songs/${s.id}/stream`;

    return {
      id: String(s.id),
      title: s.title,
      artist: s.artist ?? 'Desconocido',
      album: s.album ?? 'Desconocido',
      genre: s.genre ?? 'General',
      duration: parseDurationToSeconds(s.duration),
      coverUrl: cover,
      audioUrl: streamUrl, 
    };
  });

  return mapped;
}

export default { fetchSongs };
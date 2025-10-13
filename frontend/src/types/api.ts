export interface SongInputModel {
  title: string;
  artist: string;
  album?: string;
  genre?: string;
  duration?: string; // e.g. "3:45"
  file: string; // may be base64 data URI or filename depending on backend
  cover?: string | null; // base64 data URI
}

export interface SongResponse {
  id: number | string;
  title: string;
  artist: string;
  album?: string;
  genre?: string;
  duration?: string; // backend returns duration like "3:45"
  file?: string; // data URI (base64) or URL
  cover?: string | null; // data URI (base64) or URL
}

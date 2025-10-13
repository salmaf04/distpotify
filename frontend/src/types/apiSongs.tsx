export type ApiSong = {
  id: number | string;
  title: string;
  artist: string;
  album: string;
  genre: string;
  duration: string; // e.g. "3:45"
  file?: string; // data URL
  cover?: string; // data URL
};
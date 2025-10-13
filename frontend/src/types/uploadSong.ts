export type UploadItem = {
  file: File;
  title?: string;
  artist?: string;
  album?: string;
  genre?: string;
  coverFile?: File | null;
  duration?: number | null;
};
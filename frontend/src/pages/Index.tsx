import { useState, useMemo, useEffect } from "react";
import { Box, ThemeProvider, createTheme, CssBaseline } from "@mui/material";
import { toast } from "sonner";
import type { Song } from "../types/song";
import songService from "../services/songService";
import uploadService from "../services/uploadService";
import Navbar from "../components/Navbar";
import SongList from "../components/SongList";
import MusicPlayer from "../components/MusicPlayer";


const darkTheme = createTheme({
  palette: {
    mode: 'dark',
    primary: {
      main: 'hsl(141, 73%, 42%)',
    },
    background: {
      default: 'hsl(0, 0%, 7%)',
      paper: 'hsl(0, 0%, 10%)',
    },
  },
});

const Index = () => {
  // only show songs returned by backend GET
  const [remoteSongs, setRemoteSongs] = useState<Song[]>([]);
  const [searchQuery, setSearchQuery] = useState("");
  const [currentSong, setCurrentSong] = useState<Song | null>(null);

  const filteredSongs = useMemo(() => {
    if (!searchQuery.trim()) return remoteSongs;

    const query = searchQuery.toLowerCase();
    return remoteSongs.filter(
      (song) =>
        song.title.toLowerCase().includes(query) ||
        song.artist.toLowerCase().includes(query) ||
        song.album.toLowerCase().includes(query) ||
        song.genre.toLowerCase().includes(query)
    );
  }, [remoteSongs, searchQuery]);

  const handleSearch = (query: string) => {
    setSearchQuery(query);
  };

  type UploadItem = {
    file: File;
    title?: string;
    artist?: string;
    album?: string;
    genre?: string;
    coverFile?: File | null;
    duration?: number | null;
  };

  // (file list upload is handled by uploadService.uploadFiles in production)

  // Note: we will only add uploaded songs to the UI if the backend returns them.

  const handleUpload = (input: FileList | UploadItem[] | null) => {
    if (!input) return;
    (async () => {
      try {
        const res = await uploadService.uploadFiles(input as FileList | UploadItem[]);
  if (res.success) {
          // After a successful upload, refresh the full list from backend (authoritative)
          try {
            const fresh = await songService.fetchSongs();
            setRemoteSongs(fresh);
            const count = res.data ? res.data.length : 1;
            toast.success(`${count} canción(es) agregada(s) exitosamente`);
            return;
          } catch {
            // If fetching fresh list fails, fallback to inserting the returned items from upload
            if (res.data && res.data.length > 0) {
              setRemoteSongs((prev) => [...res.data!, ...prev]);
              toast.success(`${res.data.length} canción(es) agregada(s) exitosamente (fallback)`);
              return;
            }
            toast.success(`Subida completa pero no se pudo refrescar la lista`);
            return;
          }
  }
  // show backend/client error if provided
  toast.error(res.error ?? "No se encontraron archivos de audio válidos o la subida falló");
      } catch (err) {
        console.error(err);
        toast.error("Error subiendo canción(es)");
      }
    })();
  };

  useEffect(() => {
    // load songs from backend on mount; if it fails keep mockSongs
    const controller = new AbortController();
    (async () => {
      try {
        const remote = await songService.fetchSongs(controller.signal);
        if (remote && remote.length > 0) setRemoteSongs(remote);
      } catch (err) {
        // leave remoteSongs empty; UI will show 'No se encontraron canciones'
        console.warn('Could not fetch songs from backend', err);
      }
    })();

    return () => controller.abort();
  }, []);

  const handlePlaySong = (song: Song) => {
    setCurrentSong(song);
  };

  const handleNext = () => {
    if (!currentSong) return;
    const currentIndex = filteredSongs.findIndex((s) => s.id === currentSong.id);
    const nextIndex = (currentIndex + 1) % filteredSongs.length;
    setCurrentSong(filteredSongs[nextIndex]);
  };

  const handlePrevious = () => {
    if (!currentSong) return;
    const currentIndex = filteredSongs.findIndex((s) => s.id === currentSong.id);
    const previousIndex = currentIndex === 0 ? filteredSongs.length - 1 : currentIndex - 1;
    setCurrentSong(filteredSongs[previousIndex]);
  };

  return (
    <ThemeProvider theme={darkTheme}>
      <CssBaseline />
      <Box
        sx={{
          minHeight: '100vh',
          background: 'var(--gradient-bg)',
          pb: '120px',
        }}
      >
        <Navbar onSearch={handleSearch} onUpload={handleUpload} />
        <SongList
          songs={filteredSongs}
          currentSong={currentSong}
          onPlaySong={handlePlaySong}
        />
        <MusicPlayer
          currentSong={currentSong}
          onNext={handleNext}
          onPrevious={handlePrevious}
        />
      </Box>
    </ThemeProvider>
  );
};

export default Index;

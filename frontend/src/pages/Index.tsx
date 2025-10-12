import { useState, useMemo } from "react";
import { Box, ThemeProvider, createTheme, CssBaseline } from "@mui/material";
import { toast } from "sonner";
import type { Song } from "../types/song";
import { mockSongs } from "../data/mockSongs";
import Navbar from "../components/Navbar";
import SongList from "../components/SongList";
import MusicPlayer from "../components/MusicPlayer";
import { getDuration } from "../services/metadataService";

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
  const [songs, setSongs] = useState<Song[]>(mockSongs);
  const [searchQuery, setSearchQuery] = useState("");
  const [currentSong, setCurrentSong] = useState<Song | null>(null);

  const filteredSongs = useMemo(() => {
    if (!searchQuery.trim()) return songs;

    const query = searchQuery.toLowerCase();
    return songs.filter(
      (song) =>
        song.title.toLowerCase().includes(query) ||
        song.artist.toLowerCase().includes(query) ||
        song.album.toLowerCase().includes(query) ||
        song.genre.toLowerCase().includes(query)
    );
  }, [songs, searchQuery]);

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

  async function handleFileListUpload(files: FileList) {
    const newSongs: Song[] = [];
    for (const [index, file] of Array.from(files).entries()) {
      if (!file.type.startsWith("audio/")) continue;
      try {
        const duration = await getDuration(file);
        const audioUrl = URL.createObjectURL(file);
        const newSong: Song = {
          id: `uploaded-${Date.now()}-${index}`,
          title: file.name.replace(/\.[^/.]+$/, ""),
          artist: "Artista Desconocido",
          album: "Álbum Desconocido",
          genre: "Desconocido",
          duration: duration ? Math.round(duration) : 0,
          audioUrl,
        };
        newSongs.push(newSong);
      } catch (err) {
        console.error("Error obteniendo duración:", err);
      }
    }
    return newSongs;
  }

  function handleUploadItems(items: UploadItem[]) {
    const newSongs: Song[] = items
      .filter((it) => it.file?.type?.startsWith("audio/"))
      .map((it, idx) => ({
        id: `uploaded-${Date.now()}-${idx}`,
        title: it.title ?? it.file.name.replace(/\.[^/.]+$/, ""),
        artist: it.artist ?? "Artista Desconocido",
        album: it.album ?? "Álbum Desconocido",
        genre: it.genre ?? "Desconocido",
        duration: it.duration ? Math.round(it.duration) : 0,
        coverUrl: it.coverFile ? URL.createObjectURL(it.coverFile) : undefined,
        audioUrl: URL.createObjectURL(it.file),
      }));
    return newSongs;
  }

  const handleUpload = (input: FileList | UploadItem[] | null) => {
    if (!input) return;
    (async () => {
      let newSongs: Song[] = [];
      if ((input as FileList).item !== undefined) {
        newSongs = await handleFileListUpload(input as FileList);
      } else {
        newSongs = handleUploadItems(input as UploadItem[]);
      }

      if (newSongs.length > 0) {
        setSongs([...newSongs, ...songs]);
        toast.success(`${newSongs.length} canción(es) agregada(s) exitosamente`);
      } else {
        toast.error("No se encontraron archivos de audio válidos");
      }
    })();
  };

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

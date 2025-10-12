import { Box, Typography } from "@mui/material";
import SongCard from "./SongCard";
import type { Song } from "../types/song";

interface SongListProps {
  songs: Song[];
  currentSong: Song | null;
  onPlaySong: (song: Song) => void;
}

const SongList = ({ songs, currentSong, onPlaySong }: SongListProps) => {
  return (
    <Box sx={{ px: 3, py: 2 }}>
      <Typography
        variant="h5"
        sx={{
          fontWeight: 700,
          mb: 3,
          color: 'hsl(var(--foreground))',
        }}
      >
        Tus canciones
      </Typography>

      {songs.length === 0 ? (
        <Box
          sx={{
            textAlign: 'center',
            py: 8,
            color: 'hsl(var(--muted-foreground))',
          }}
        >
          <Typography variant="h6">No se encontraron canciones</Typography>
          <Typography variant="body2" sx={{ mt: 1 }}>
            Intenta con otra búsqueda o sube una canción
          </Typography>
        </Box>
      ) : (
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
          {songs.map((song) => (
            <SongCard
              key={song.id}
              song={song}
              onPlay={onPlaySong}
              isPlaying={currentSong?.id === song.id}
            />
          ))}
        </Box>
      )}
    </Box>
  );
};

export default SongList;

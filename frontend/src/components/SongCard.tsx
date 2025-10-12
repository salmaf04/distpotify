import { Card, CardContent, Typography, Box, IconButton } from "@mui/material";
import { IconPlayerPlayFilled, IconMusic } from "@tabler/icons-react";
import type { Song } from "../types/song";


interface SongCardProps {
  song: Song;
  onPlay: (song: Song) => void;
  isPlaying: boolean;
}

const SongCard = ({ song, onPlay, isPlaying }: SongCardProps) => {
  const formatDuration = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, "0")}`;
  };

  return (
    <Card
      sx={{
        bgcolor: isPlaying ? 'hsl(var(--muted))' : 'hsl(var(--card))',
        border: '1px solid hsl(var(--border))',
        borderRadius: 2,
        cursor: 'pointer',
        transition: 'var(--transition-smooth)',
        '&:hover': {
          bgcolor: 'hsl(var(--muted))',
          transform: 'translateY(-2px)',
          '& .play-button': {
            opacity: 1,
          },
        },
      }}
      onClick={() => onPlay(song)}
    >
      <CardContent sx={{ p: 2, '&:last-child': { pb: 2 } }}>
        <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
          <Box
            sx={{
              position: 'relative',
              width: 56,
              height: 56,
              borderRadius: 1,
              overflow: 'hidden',
              bgcolor: 'hsl(var(--secondary))',
              flexShrink: 0,
            }}
          >
            {song.coverUrl ? (
              <img
                src={song.coverUrl}
                alt={song.title}
                style={{
                  width: '100%',
                  height: '100%',
                  objectFit: 'cover',
                }}
              />
            ) : (
              <Box
                sx={{
                  width: '100%',
                  height: '100%',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <IconMusic size={24} color="hsl(var(--muted-foreground))" />
              </Box>
            )}
            <Box
              className="play-button"
              sx={{
                position: 'absolute',
                inset: 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                bgcolor: 'rgba(0, 0, 0, 0.6)',
                opacity: isPlaying ? 1 : 0,
                transition: 'var(--transition-smooth)',
              }}
            >
              <IconButton
                sx={{
                  bgcolor: 'hsl(var(--primary))',
                  color: 'hsl(var(--primary-foreground))',
                  '&:hover': {
                    bgcolor: 'hsl(141, 73%, 48%)',
                    transform: 'scale(1.1)',
                  },
                }}
              >
                <IconPlayerPlayFilled size={20} />
              </IconButton>
            </Box>
          </Box>

          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography
              variant="subtitle1"
              sx={{
                fontWeight: 600,
                color: isPlaying ? 'hsl(var(--primary))' : 'hsl(var(--foreground))',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {song.title}
            </Typography>
            <Typography
              variant="body2"
              sx={{
                color: 'hsl(var(--muted-foreground))',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {song.artist}
            </Typography>
          </Box>

          <Box sx={{ display: { xs: 'none', sm: 'block' }, minWidth: 120 }}>
            <Typography
              variant="body2"
              sx={{
                color: 'hsl(var(--muted-foreground))',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
            >
              {song.album}
            </Typography>
          </Box>

          <Box sx={{ display: { xs: 'none', md: 'block' }, minWidth: 100 }}>
            <Typography variant="body2" sx={{ color: 'hsl(var(--muted-foreground))' }}>
              {song.genre}
            </Typography>
          </Box>

          <Typography
            variant="body2"
            sx={{ color: 'hsl(var(--muted-foreground))', minWidth: 45, textAlign: 'right' }}
          >
            {formatDuration(song.duration)}
          </Typography>
        </Box>
      </CardContent>
    </Card>
  );
};

export default SongCard;

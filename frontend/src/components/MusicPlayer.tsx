import { useState, useEffect, useRef } from "react";
import { Box, IconButton, Typography, Slider, Paper } from "@mui/material";
import {
  IconPlayerPlayFilled,
  IconPlayerPauseFilled,
  IconPlayerSkipBackFilled,
  IconPlayerSkipForwardFilled,
  IconVolume,
  IconMusic,
} from "@tabler/icons-react";
import type { Song } from "../types/song";


interface MusicPlayerProps {
  currentSong: Song | null;
  onNext: () => void;
  onPrevious: () => void;
}

const MusicPlayer = ({ currentSong, onNext, onPrevious }: MusicPlayerProps) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [volume, setVolume] = useState(70);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const volumeRef = useRef<number>(volume);

  // initialize audio element once
  useEffect(() => {
    audioRef.current = new Audio();
    audioRef.current.preload = 'metadata';
    return () => {
      if (audioRef.current) {
        audioRef.current.pause();
        audioRef.current.src = '';
        audioRef.current = null;
      }
    };
  }, []);

  const onNextRef = useRef(onNext);
  useEffect(() => {
    onNextRef.current = onNext;
  }, [onNext]);

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio) return;

    let cancelled = false;

    const setup = async () => {
      if (currentSong?.audioUrl) {
       
        const isDifferentSrc = audio.src !== currentSong.audioUrl;
        if (isDifferentSrc) {
          audio.src = currentSong.audioUrl;
          audio.currentTime = 0;
        }
        audio.volume = volumeRef.current / 100;
        try {
          if (isDifferentSrc) {
            await audio.play();
            if (!cancelled) setIsPlaying(true);
          }
        } catch {
          if (!cancelled) setIsPlaying(false);
        }
      } else {
        audio.pause();
        if (!cancelled) setIsPlaying(false);
        if (!cancelled) setCurrentTime(0);
        audio.src = '';
      }
    };

    setup();

    const onTimeUpdate = () => setCurrentTime(audio.currentTime || 0);
    const onEnded = () => {
      setIsPlaying(false);
      if (onNextRef.current) onNextRef.current();
    };

    audio.addEventListener('timeupdate', onTimeUpdate);
    audio.addEventListener('ended', onEnded);

    return () => {
      cancelled = true;
      audio.removeEventListener('timeupdate', onTimeUpdate);
      audio.removeEventListener('ended', onEnded);
    };
  }, [currentSong]);

  useEffect(() => {
    volumeRef.current = volume;
    if (audioRef.current) audioRef.current.volume = volume / 100;
  }, [volume]);

  const togglePlayPause = () => {
    const audio = audioRef.current;
    if (!audio) return;
    if (isPlaying) {
      audio.pause();
      setIsPlaying(false);
    } else {
      const playPromise = audio.play();
      playPromise.then(() => setIsPlaying(true)).catch(() => setIsPlaying(false));
    }
  };

  const handleTimeChange = (_: Event, value: number | number[]) => {
    const t = value as number;
    const audio = audioRef.current;
    if (audio) audio.currentTime = t;
    setCurrentTime(t);
  };

  const handleVolumeChange = (_: Event, value: number | number[]) => {
    const v = value as number;
    setVolume(v);
    if (audioRef.current) audioRef.current.volume = v / 100;
  };

  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60);
    const secs = Math.floor(seconds % 60);
    return `${mins}:${secs.toString().padStart(2, "0")}`;
  };

  if (!currentSong) {
    return (
      <Paper
        elevation={8}
        sx={{
          position: 'fixed',
          bottom: 0,
          left: 0,
          right: 0,
          bgcolor: 'hsl(var(--card))',
          borderTop: '1px solid hsl(var(--border))',
          p: 2,
          zIndex: 1000,
        }}
      >
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'hsl(var(--muted-foreground))',
            py: 1,
          }}
        >
          <Typography variant="body2">Selecciona una canción para reproducir</Typography>
        </Box>
      </Paper>
    );
  }

  return (
    <Paper
      elevation={8}
      sx={{
        position: 'fixed',
        bottom: 0,
        left: 0,
        right: 0,
        bgcolor: 'hsl(var(--card))',
        borderTop: '1px solid hsl(var(--border))',
        p: 2,
        zIndex: 1000,
      }}
    >
      <Box sx={{ maxWidth: 1400, mx: 'auto' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 3 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, minWidth: 200, flex: 1 }}>
            <Box
              sx={{
                width: 56,
                height: 56,
                borderRadius: 1,
                overflow: 'hidden',
                bgcolor: 'hsl(var(--secondary))',
                flexShrink: 0,
              }}
            >
              {currentSong.coverUrl ? (
                <img
                  src={currentSong.coverUrl}
                  alt={currentSong.title}
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
            </Box>
            <Box sx={{ minWidth: 0 }}>
              <Typography
                variant="subtitle2"
                sx={{
                  fontWeight: 600,
                  color: 'hsl(var(--foreground))',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {currentSong.title}
              </Typography>
              <Typography
                variant="caption"
                sx={{
                  color: 'hsl(var(--muted-foreground))',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  display: 'block',
                }}
              >
                {currentSong.artist}
              </Typography>
            </Box>
          </Box>
          <Box sx={{ flex: 2, maxWidth: 600 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 1, mb: 1 }}>
              <IconButton
                onClick={onPrevious}
                sx={{
                  color: 'hsl(var(--foreground))',
                  '&:hover': { color: 'hsl(var(--primary))' },
                }}
              >
                <IconPlayerSkipBackFilled size={20} />
              </IconButton>
              <IconButton
                onClick={togglePlayPause}
                sx={{
                  bgcolor: 'hsl(var(--primary))',
                  color: 'hsl(var(--primary-foreground))',
                  '&:hover': {
                    bgcolor: 'hsl(141, 73%, 48%)',
                    transform: 'scale(1.05)',
                  },
                }}
              >
                {isPlaying ? (
                  <IconPlayerPauseFilled size={24} />
                ) : (
                  <IconPlayerPlayFilled size={24} />
                )}
              </IconButton>
              <IconButton
                onClick={onNext}
                sx={{
                  color: 'hsl(var(--foreground))',
                  '&:hover': { color: 'hsl(var(--primary))' },
                }}
              >
                <IconPlayerSkipForwardFilled size={20} />
              </IconButton>
            </Box>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
              <Typography variant="caption" sx={{ color: 'hsl(var(--muted-foreground))', minWidth: 40 }}>
                {formatTime(Math.floor(currentTime))}
              </Typography>
              <Slider
                value={currentTime}
                max={currentSong.duration || (audioRef.current?.duration || 0)}
                onChange={handleTimeChange}
                sx={{
                  color: 'hsl(var(--primary))',
                  '& .MuiSlider-thumb': {
                    width: 12,
                    height: 12,
                    '&:hover, &.Mui-focusVisible': {
                      boxShadow: '0 0 0 8px rgba(30, 215, 96, 0.16)',
                    },
                  },
                  '& .MuiSlider-track': {
                    height: 4,
                  },
                  '& .MuiSlider-rail': {
                    height: 4,
                    bgcolor: 'hsl(var(--muted))',
                  },
                }}
              />
              <Typography variant="caption" sx={{ color: 'hsl(var(--muted-foreground))', minWidth: 40 }}>
                {formatTime(Math.floor(currentSong.duration))}
              </Typography>
            </Box>
          </Box>
          <Box sx={{ display: { xs: 'none', md: 'flex' }, alignItems: 'center', gap: 1, minWidth: 150, flex: 1, justifyContent: 'flex-end' }}>
            <IconVolume size={20} color="hsl(var(--muted-foreground))" />
            <Slider
              value={volume}
              onChange={handleVolumeChange}
              sx={{
                maxWidth: 100,
                color: 'hsl(var(--primary))',
                '& .MuiSlider-thumb': {
                  width: 12,
                  height: 12,
                },
                '& .MuiSlider-track': {
                  height: 4,
                },
                '& .MuiSlider-rail': {
                  height: 4,
                  bgcolor: 'hsl(var(--muted))',
                },
              }}
            />
          </Box>
        </Box>
      </Box>
    </Paper>
  );
};

export default MusicPlayer;
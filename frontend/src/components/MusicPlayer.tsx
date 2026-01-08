import { useState, useEffect, useRef } from "react";
import { Box, IconButton, Typography, Slider, Paper } from "@mui/material";
import {
  IconPlayerPlayFilled,
  IconPlayerPauseFilled,
  IconPlayerSkipBackFilled,
  IconPlayerSkipForwardFilled,
  IconVolume,
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
  const [duration, setDuration] = useState(0); // Estado local para duración real del audio
  const [volume, setVolume] = useState(70);
  
  const audioRef = useRef<HTMLAudioElement | null>(null);

  // Inicializar audio una vez
  useEffect(() => {
    audioRef.current = new Audio();
    // 'metadata' es suficiente para streaming, no descarga todo
    audioRef.current.preload = 'metadata'; 
    
    // Eventos del audio
    const handleTimeUpdate = () => {
      if (audioRef.current) setCurrentTime(audioRef.current.currentTime);
    };
    
    const handleLoadedMetadata = () => {
      if (audioRef.current) {
        setDuration(audioRef.current.duration);
        // Si ya estaba reproduciendo, intenta seguir (útil al cambiar de canción)
        if (isPlaying) audioRef.current.play().catch(console.error);
      }
    };

    const handleEnded = () => {
      setIsPlaying(false);
      onNext(); // Auto-play siguiente
    };

    audioRef.current.addEventListener('timeupdate', handleTimeUpdate);
    audioRef.current.addEventListener('loadedmetadata', handleLoadedMetadata);
    audioRef.current.addEventListener('ended', handleEnded);

    return () => {
      if (audioRef.current) {
        audioRef.current.pause();
        audioRef.current.removeEventListener('timeupdate', handleTimeUpdate);
        audioRef.current.removeEventListener('loadedmetadata', handleLoadedMetadata);
        audioRef.current.removeEventListener('ended', handleEnded);
      }
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []); // Solo al montar

  // Efecto cuando cambia la canción
  useEffect(() => {
    if (audioRef.current && currentSong) {
      // Asignamos la URL del stream
      audioRef.current.src = currentSong.audioUrl || '';
      audioRef.current.load();
      
      // Reiniciamos estados visuales
      setCurrentTime(0);
      setIsPlaying(true);
      
      // Intentar reproducir
      audioRef.current.play().catch(e => {
        console.warn("Autoplay bloqueado o fallido:", e);
        setIsPlaying(false);
      });
    } else if (audioRef.current && !currentSong) {
      audioRef.current.pause();
      setIsPlaying(false);
    }
  }, [currentSong]);

  // Manejar Play/Pause manual
  const togglePlay = () => {
    if (!audioRef.current || !currentSong) return;
    
    if (isPlaying) {
      audioRef.current.pause();
    } else {
      audioRef.current.play();
    }
    setIsPlaying(!isPlaying);
  };

  const handleVolumeChange = (_: Event, newValue: number | number[]) => {
    const val = Array.isArray(newValue) ? newValue[0] : newValue;
    setVolume(val);
    if (audioRef.current) {
      audioRef.current.volume = val / 100;
    }
  };

  const handleSeek = (_: Event, newValue: number | number[]) => {
    const val = Array.isArray(newValue) ? newValue[0] : newValue;
    setCurrentTime(val);
    if (audioRef.current) {
      audioRef.current.currentTime = val;
    }
  };

  const formatTime = (time: number) => {
    if (!time || isNaN(time)) return "0:00";
    const minutes = Math.floor(time / 60);
    const seconds = Math.floor(time % 60);
    return `${minutes}:${seconds.toString().padStart(2, '0')}`;
  };

  if (!currentSong) return null;

  return (
    <Paper 
      sx={{ 
        position: 'fixed', 
        bottom: 0, 
        left: 0, 
        right: 0, 
        p: 2, 
        bgcolor: 'hsl(var(--card))',
        borderTop: '1px solid hsl(var(--border))',
        display: 'flex',
        alignItems: 'center',
        gap: 2,
        zIndex: 1000
      }}
      elevation={3}
    >
      {/* Info Canción */}
      <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, width: '30%' }}>
        {currentSong.coverUrl && (
          <Box 
            component="img" 
            src={currentSong.coverUrl} 
            sx={{ width: 56, height: 56, borderRadius: 1, objectFit: 'cover' }} 
          />
        )}
        <Box sx={{ overflow: 'hidden' }}>
          <Typography variant="subtitle2" noWrap>{currentSong.title}</Typography>
          <Typography variant="caption" color="text.secondary" noWrap>{currentSong.artist}</Typography>
        </Box>
      </Box>

      {/* Controles Centrales */}
      <Box sx={{ flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 1 }}>
          <IconButton onClick={onPrevious}><IconPlayerSkipBackFilled /></IconButton>
          <IconButton onClick={togglePlay} size="large" sx={{ bgcolor: 'hsl(var(--primary))', color: 'white', '&:hover': { bgcolor: 'hsl(141, 73%, 45%)' } }}>
            {isPlaying ? <IconPlayerPauseFilled /> : <IconPlayerPlayFilled />}
          </IconButton>
          <IconButton onClick={onNext}><IconPlayerSkipForwardFilled /></IconButton>
        </Box>
        
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, width: '100%', maxWidth: 500 }}>
          <Typography variant="caption">{formatTime(currentTime)}</Typography>
          <Slider 
            size="small" 
            value={currentTime} 
            max={duration || currentSong.duration || 100} 
            onChange={handleSeek} 
            sx={{ color: 'hsl(var(--primary))' }}
          />
          <Typography variant="caption">{formatTime(duration || currentSong.duration)}</Typography>
        </Box>
      </Box>

      {/* Volumen */}
      <Box sx={{ width: '30%', display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: 1 }}>
        <IconVolume size={20} />
        <Slider 
          size="small" 
          value={volume} 
          onChange={handleVolumeChange} 
          sx={{ width: 100, color: 'hsl(var(--primary))' }} 
        />
      </Box>
    </Paper>
  );
};

export default MusicPlayer;
import { useState } from "react";
import { IconSearch, IconUpload } from "@tabler/icons-react";
import {
  TextField,
  Button,
  AppBar,
  Toolbar,
  Box,
  IconButton,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Avatar,
  Typography,
} from "@mui/material";
import { getDuration } from "../services/metadataService";

interface UploadItem {
  file: File;
  title?: string;
  artist?: string;
  album?: string;
  genre?: string;
  coverFile?: File | null;
  duration?: number | null;
}

interface NavbarProps {
  onSearch: (query: string) => void;
  onUpload: (items: FileList | UploadItem[]) => void;
}

const Navbar = ({ onSearch, onUpload }: NavbarProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [uploadItems, setUploadItems] = useState<UploadItem[]>([]);

  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = e.target.value;
    setSearchQuery(value);
    onSearch(value);
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;

    const items: UploadItem[] = Array.from(files).map((f) => ({ file: f, title: f.name.replace(/\.[^/.]+$/, "") }));
    // compute durations asynchronously, then open modal
    (async () => {
      const withDur = await Promise.all(
        items.map(async (it) => ({ ...it, duration: await getDuration(it.file) }))
      );
      setUploadItems(withDur);
      setOpen(true);
    })();
  };

  const handleClose = () => {
    setOpen(false);
    setUploadItems([]);
  };

  const handleCoverChange = (index: number, file?: File | null) => {
    setUploadItems((prev) => {
      const copy = [...prev];
      copy[index] = { ...copy[index], coverFile: file || null };
      return copy;
    });
  };

  const handleFieldChange = (index: number, field: keyof UploadItem, value: unknown) => {
    setUploadItems((prev) => {
      const copy = [...prev];
      copy[index] = { ...copy[index], [field]: value };
      return copy;
    });
  };

  const handleConfirm = () => {
    onUpload(uploadItems);
    handleClose();
  };

  return (
    <AppBar 
      position="sticky" 
      sx={{ 
        bgcolor: 'hsl(var(--card))',
        borderBottom: '1px solid hsl(var(--border))',
        boxShadow: 'none',
      }}
    >
      <Toolbar sx={{ gap: 2, py: 1 }}>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Box
            component="span"
            sx={{
              fontSize: '1.5rem',
              fontWeight: 700,
              background: 'linear-gradient(135deg, hsl(var(--primary)) 0%, hsl(141, 73%, 55%) 100%)',
              WebkitBackgroundClip: 'text',
              WebkitTextFillColor: 'transparent',
              backgroundClip: 'text',
            }}
          >
            Dispotify
          </Box>
        </Box>

        <Box sx={{ flex: 1, maxWidth: 600, mx: 'auto' }}>
          <TextField
            fullWidth
            size="small"
            placeholder="Buscar canciones, artistas o álbumes..."
            value={searchQuery}
            onChange={handleSearchChange}
            inputProps={{
              startAdornment: (
                <IconButton size="small" sx={{ mr: 0.5 }}>
                  <IconSearch size={20} color="hsl(var(--muted-foreground))" />
                </IconButton>
              ),
              sx: {
                bgcolor: 'hsl(var(--secondary))',
                borderRadius: '500px',
                '& fieldset': { border: 'none' },
                '&:hover': {
                  bgcolor: 'hsl(var(--muted))',
                },
                color: 'hsl(var(--foreground))',
              },
            }}
          />
        </Box>

        <Button
          variant="contained"
          component="label"
          startIcon={<IconUpload size={20} />}
          sx={{
            bgcolor: 'hsl(var(--primary))',
            color: 'hsl(var(--primary-foreground))',
            borderRadius: '500px',
            textTransform: 'none',
            fontWeight: 600,
            px: 3,
            '&:hover': {
              bgcolor: 'hsl(141, 73%, 48%)',
              transform: 'scale(1.02)',
            },
            transition: 'var(--transition-smooth)',
          }}
        >
          Subir canción
          <input
            type="file"
            hidden
            accept="audio/*"
            multiple
            onChange={handleFileUpload}
          />
        </Button>
        {/* Metadata modal */}
        <Dialog open={open} onClose={handleClose} maxWidth="md" fullWidth>
          <DialogTitle>Editar metadatos de subida</DialogTitle>
          <DialogContent>
            {uploadItems.map((item, idx) => (
              <Box key={`${item.file.name}-${idx}`} sx={{ mb: 2, borderBottom: '1px solid hsl(var(--border))', pb: 2 }}>
                <Box sx={{ display: 'flex', gap: 2, alignItems: 'center' }}>
                  <Avatar variant="square" sx={{ width: 64, height: 64 }} src={item.coverFile ? URL.createObjectURL(item.coverFile) : undefined} />
                  <Box sx={{ flex: 1 }}>
                    <TextField fullWidth label="Título" value={item.title || ''} onChange={(e) => handleFieldChange(idx, 'title', e.target.value)} />
                    <Box sx={{ display: 'flex', gap: 1, mt: 1 }}>
                      <TextField fullWidth label="Artista" value={item.artist || ''} onChange={(e) => handleFieldChange(idx, 'artist', e.target.value)} />
                      <TextField fullWidth label="Álbum" value={item.album || ''} onChange={(e) => handleFieldChange(idx, 'album', e.target.value)} />
                      <TextField fullWidth label="Género" value={item.genre || ''} onChange={(e) => handleFieldChange(idx, 'genre', e.target.value)} />
                    </Box>
                    <Box sx={{ display: 'flex', gap: 1, alignItems: 'center', mt: 1 }}>
                      <Typography variant="body2">Duración: {item.duration ? Math.round(item.duration) + 's' : 'desconocida'}</Typography>
                      <Button variant="outlined" component="label" size="small">
                        Subir portada
                        <input type="file" hidden accept="image/*" onChange={(e) => handleCoverChange(idx, e.target.files ? e.target.files[0] : undefined)} />
                      </Button>
                    </Box>
                  </Box>
                </Box>
              </Box>
            ))}
          </DialogContent>
          <DialogActions>
            <Button onClick={handleClose}>Cancelar</Button>
            <Button variant="contained" onClick={handleConfirm}>Confirmar y agregar</Button>
          </DialogActions>
        </Dialog>
      </Toolbar>
    </AppBar>
  );
};

export default Navbar;

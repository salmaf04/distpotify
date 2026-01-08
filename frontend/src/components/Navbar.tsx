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
import { useAuth } from "../hooks/useAuth";

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
  const { user, login, register, logout } = useAuth();
  const [searchQuery, setSearchQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [uploadItems, setUploadItems] = useState<UploadItem[]>([]);
  const [openLogin, setOpenLogin] = useState(false);
  const [openRegister, setOpenRegister] = useState(false);
  const [authUser, setAuthUser] = useState("");
  const [authPass, setAuthPass] = useState("");
  const [authRole, setAuthRole] = useState("user");

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

  const handleOpenLogin = () => {
    setAuthUser(''); setAuthPass(''); setOpenLogin(true);
  };

  const handleLoginSubmit = async () => {
    const resp = await login(authUser, authPass);
    if (resp.ok) setOpenLogin(false);
    else alert('Login falló: ' + (resp.error ?? JSON.stringify(resp.data)));
  };

  const handleOpenRegister = () => {
    setAuthUser(''); setAuthPass(''); setAuthRole('user'); setOpenRegister(true);
  };

  const handleRegisterSubmit = async () => {
    const resp = await register(authUser, authPass, authRole);
    if (resp.ok) setOpenRegister(false);
    else alert('Register falló: ' + (resp.error ?? JSON.stringify(resp.data)));
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

        {user && user.role === 'admin' ? (
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
        ) : (
          <Button onClick={() => user ? alert('Acceso restringido: solo administradores pueden subir canciones') : handleOpenLogin()} variant="outlined">Iniciar sesión</Button>
        )}

        {user ? (
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
            <Typography variant="body2">{user.username} ({user.role})</Typography>
            <Button onClick={() => { logout(); }} size="small">Salir</Button>
          </Box>
        ) : (
          <Box sx={{ display: 'flex', gap: 1 }}>
            <Button onClick={handleOpenLogin}>Login</Button>
            <Button onClick={handleOpenRegister}>Register</Button>
          </Box>
        )}
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

        {/* Login dialog */}
        <Dialog open={openLogin} onClose={() => setOpenLogin(false)}>
          <DialogTitle>Iniciar sesión</DialogTitle>
          <DialogContent>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1, width: 320 }}>
              <TextField label="Usuario" value={authUser} onChange={(e) => setAuthUser(e.target.value)} />
              <TextField label="Contraseña" type="password" value={authPass} onChange={(e) => setAuthPass(e.target.value)} />
            </Box>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setOpenLogin(false)}>Cancelar</Button>
            <Button variant="contained" onClick={handleLoginSubmit}>Iniciar</Button>
          </DialogActions>
        </Dialog>

        {/* Register dialog */}
        <Dialog open={openRegister} onClose={() => setOpenRegister(false)}>
          <DialogTitle>Crear cuenta</DialogTitle>
          <DialogContent>
            <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1, width: 320 }}>
              <TextField label="Usuario" value={authUser} onChange={(e) => setAuthUser(e.target.value)} />
              <TextField label="Contraseña" type="password" value={authPass} onChange={(e) => setAuthPass(e.target.value)} />
              <TextField label="Role (admin|user)" value={authRole} onChange={(e) => setAuthRole(e.target.value)} />
            </Box>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setOpenRegister(false)}>Cancelar</Button>
            <Button variant="contained" onClick={handleRegisterSubmit}>Crear</Button>
          </DialogActions>
        </Dialog>
      </Toolbar>
    </AppBar>
  );
};

export default Navbar;

import React, { useState } from 'react';
import { Box, Button, TextField, Paper, Typography } from '@mui/material';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';

const LoginPage: React.FC = () => {
  const { login } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const navigate = useNavigate();

  const handleSubmit = async () => {
    const resp = await login(username, password);
    if (resp.ok) {
      navigate('/');
    } else {
      alert('Login failed: ' + (resp.error ?? JSON.stringify(resp.data)));
    }
  };

  return (
    <Box sx={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
      <Paper sx={{ width: 400, p: 3 }}>
        <Typography variant="h6" sx={{ mb: 2 }}>Iniciar sesión</Typography>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField label="Usuario" value={username} onChange={(e) => setUsername(e.target.value)} />
          <TextField label="Contraseña" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          <Button variant="contained" onClick={handleSubmit}>Entrar</Button>
          <Button variant="outlined" onClick={() => navigate('/register')}>Registrarse</Button>
        </Box>
      </Paper>
    </Box>
  );
};

export default LoginPage;

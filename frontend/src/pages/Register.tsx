import React, { useState } from 'react';
import { Box, Button, TextField, Paper, Typography, MenuItem, Select, InputLabel, FormControl } from '@mui/material';
import { useNavigate } from 'react-router-dom';
import authService from '../services/authService';

const RegisterPage: React.FC = () => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState('user');
  const navigate = useNavigate();

  const handleSubmit = async () => {
    const resp = await authService.register(username, password, role);
    if (resp.ok) {
      alert('Registro exitoso. Ahora puedes iniciar sesión.');
      navigate('/login');
    } else {
      alert('Registro fallido: ' + (resp.data ?? JSON.stringify(resp)));
    }
  };

  return (
    <Box sx={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', p: 2 }}>
      <Paper sx={{ width: 420, p: 3 }}>
        <Typography variant="h6" sx={{ mb: 2 }}>Registro</Typography>
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField label="Usuario" value={username} onChange={(e) => setUsername(e.target.value)} />
          <FormControl>
            <InputLabel id="role-label">Rol</InputLabel>
            <Select labelId="role-label" value={role} label="Rol" onChange={(e) => setRole(String(e.target.value))}>
              <MenuItem value="user">Usuario</MenuItem>
              <MenuItem value="admin">Administrador</MenuItem>
            </Select>
          </FormControl>
          <TextField label="Contraseña" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          <Button variant="contained" onClick={handleSubmit}>Registrarse</Button>
          <Button variant="text" onClick={() => navigate('/login')}>Volver al login</Button>
        </Box>
      </Paper>
    </Box>
  );
};

export default RegisterPage;

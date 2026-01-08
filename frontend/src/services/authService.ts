type User = {
  id: string | number;
  username: string;
  role: string;
};

const API_BASE = (import.meta.env.VITE_API_URL || '').replace(/\/$/, '');

async function post(path: string, body: unknown) {
  const url = API_BASE ? `${API_BASE}${path}` : path;
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include', // ensure cookies (session) are stored
    body: JSON.stringify(body),
  });
  const text = await res.text();
  let data: unknown = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  return { ok: res.ok, status: res.status, data };
}

async function register(username: string, password: string, role: string) {
  const resp = await post('/auth/register', { username, password, role });
  return resp;
}

async function login(username: string, password: string) {
  const resp = await post('/auth/login', { username, password });
  return resp;
}

function saveUser(user: User | null) {
  if (!user) return localStorage.removeItem('currentUser');
  localStorage.setItem('currentUser', JSON.stringify(user));
}

function loadUser(): User | null {
  const raw = localStorage.getItem('currentUser');
  if (!raw) return null;
  try { return JSON.parse(raw) as User; } catch { return null; }
}

export default {
  register,
  login,
  saveUser,
  loadUser,
};

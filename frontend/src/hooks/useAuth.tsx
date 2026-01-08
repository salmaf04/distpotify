import React, { createContext, useContext, useEffect, useState } from 'react';
import authService from '../services/authService';

type User = {
  id: string | number;
  username: string;
  role: string;
};

type AuthContextValue = {
  user: User | null;
  login: (username: string, password: string) => Promise<{ ok: boolean; data?: any; error?: string }>;
  register: (username: string, password: string, role: string) => Promise<{ ok: boolean; data?: any; error?: string }>;
  logout: () => void;
};

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [user, setUser] = useState<User | null>(() => authService.loadUser());

  useEffect(() => {
    // keep localStorage in sync
    authService.saveUser(user);
  }, [user]);

  const login = async (username: string, password: string) => {
    const resp = await authService.login(username, password);
    if (resp.ok) {
      // backend returns user inside resp.data or resp.data.user
      const d: any = resp.data ?? {};
      const maybeUser = d.user ?? d;
      if (maybeUser && (maybeUser.username || maybeUser.id)) {
        const u: User = { id: maybeUser.id, username: maybeUser.username, role: maybeUser.role };
        setUser(u);
        return { ok: true, data: u };
      }
      return { ok: true, data: d };
    }
    return { ok: false, error: typeof resp.data === 'string' ? resp.data : JSON.stringify(resp.data) };
  };

  const register = async (username: string, password: string, role: string) => {
    const resp = await authService.register(username, password, role);
    if (resp.ok) {
      const d: any = resp.data ?? {};
      const maybeUser = d.user ?? d;
      if (maybeUser && (maybeUser.username || maybeUser.id)) {
        const u: User = { id: maybeUser.id, username: maybeUser.username, role: maybeUser.role };
        setUser(u);
        return { ok: true, data: u };
      }
      return { ok: true, data: d };
    }
    return { ok: false, error: typeof resp.data === 'string' ? resp.data : JSON.stringify(resp.data) };
  };

  const logout = () => {
    setUser(null);
    // we can't reliably clear server session without endpoint; local-only logout
    authService.saveUser(null);
  };

  return (
    <AuthContext.Provider value={{ user, login, register, logout }}>{children}</AuthContext.Provider>
  );
};

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

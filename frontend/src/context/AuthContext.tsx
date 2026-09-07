import React, { createContext, useContext, useEffect, useState } from 'react';
import { api } from '../services/api';
import { wsClient } from '../services/websocket';
import { User } from '../types/api';

interface AuthContextType {
  user: User | null;
  token: string | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  quickLoginDemo: (player: 'player1' | 'player2') => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(api.getToken());
  const [loading, setLoading] = useState<boolean>(true);

  // Sync token to WebSocket client
  useEffect(() => {
    if (token) {
      wsClient.connect(token);
    } else {
      wsClient.disconnect();
    }
  }, [token]);

  // Load current user profile on mount
  useEffect(() => {
    async function loadUser() {
      if (!token) {
        setLoading(false);
        return;
      }
      try {
        const me = await api.getMe();
        setUser(me);
      } catch (err) {
        console.warn('Session expired or invalid:', err);
        api.setToken(null);
        setToken(null);
        setUser(null);
      } finally {
        setLoading(false);
      }
    }

    loadUser();
  }, [token]);

  const login = async (email: string, password: string) => {
    const res = await api.login(email, password);
    setToken(res.token);
    setUser(res.user);
  };

  const register = async (email: string, password: string) => {
    const res = await api.register(email, password);
    setToken(res.token);
    setUser(res.user);
  };

  const quickLoginDemo = async (player: 'player1' | 'player2') => {
    const email = `${player}@codeduel.dev`;
    const password = 'password123';
    try {
      await login(email, password);
    } catch {
      // If user does not exist yet, register them
      await register(email, password);
    }
  };

  const logout = () => {
    api.setToken(null);
    setToken(null);
    setUser(null);
    wsClient.disconnect();
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        token,
        loading,
        login,
        register,
        quickLoginDemo,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = (): AuthContextType => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};

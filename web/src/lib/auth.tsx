/* eslint-disable react-refresh/only-export-components --
   This module intentionally exports the AuthProvider component alongside the
   useAuth hook: they are one cohesive unit and always change together. */
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { api, ApiError } from './api';
import type { User } from './types';

type AuthState =
  | { status: 'loading' }
  | { status: 'authenticated'; user: User }
  | { status: 'anonymous' };

interface AuthContextValue {
  state: AuthState;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: 'loading' });

  // Resolve the current session once on mount. A 401 simply means "not logged
  // in"; anything else is surfaced as anonymous too (the login screen is the
  // safe fallback).
  useEffect(() => {
    let active = true;
    api
      .me()
      .then((user) => {
        if (active) {
          setState({ status: 'authenticated', user });
        }
      })
      .catch(() => {
        if (active) {
          setState({ status: 'anonymous' });
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    const user = await api.login(username, password);
    setState({ status: 'authenticated', user });
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } catch (err) {
      // A failed logout call (e.g. already-expired session) still ends the
      // client session; only unexpected errors are worth noting.
      if (!(err instanceof ApiError)) {
        throw err;
      }
    }
    setState({ status: 'anonymous' });
  }, []);

  const value = useMemo<AuthContextValue>(() => ({ state, login, logout }), [state, login, logout]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}

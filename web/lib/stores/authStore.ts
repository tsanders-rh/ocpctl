import { create } from "zustand";
import type { User } from "@/types/api";

type AuthMode = "jwt" | "iam";

interface AuthState {
  user: User | null;
  accessToken: string | null;
  authMode: AuthMode;
  isAuthenticated: boolean;
  isLoading: boolean;

  // Actions
  setUser: (user: User | null) => void;
  setAccessToken: (token: string | null) => void;
  setLoading: (loading: boolean) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  accessToken: null,
  authMode: (process.env.NEXT_PUBLIC_AUTH_MODE as AuthMode) || "jwt",
  isAuthenticated: false,
  isLoading: true,

  setUser: (user) =>
    set({
      user,
      isAuthenticated: !!user,
    }),

  setAccessToken: (token) => {
    console.log('[AuthStore] Setting access token:', {
      hasToken: !!token,
      tokenLength: token?.length || 0,
    });
    set({
      accessToken: token,
    });
  },

  setLoading: (loading) =>
    set({
      isLoading: loading,
    }),

  logout: () =>
    set({
      user: null,
      accessToken: null,
      isAuthenticated: false,
    }),
}));

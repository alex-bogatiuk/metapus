import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { AuthUserResponse, TokenResponse } from "@/types/auth"

interface AuthState {
  tokens: TokenResponse | null
  user: AuthUserResponse | null
  isAuthenticated: boolean
}

interface AuthActions {
  setAuth: (tokens: TokenResponse, user: AuthUserResponse) => void
  setUser: (user: AuthUserResponse) => void
  setTokens: (tokens: TokenResponse) => void
  logout: () => void
}

export const useAuthStore = create<AuthState & AuthActions>()(
  persist(
    (set) => ({
      tokens: null,
      user: null,
      isAuthenticated: false,

      setAuth: (tokens, user) =>
        set({ tokens, user, isAuthenticated: true }),

      setUser: (user) =>
        set({ user }),

      setTokens: (tokens) =>
        set({ tokens }),

      logout: () =>
        set({ tokens: null, user: null, isAuthenticated: false }),
    }),
    {
      name: "metapus-auth",
      partialize: (state) => ({
        tokens: state.tokens,
        user: state.user,
        isAuthenticated: state.isAuthenticated,
      }),
    }
  )
)

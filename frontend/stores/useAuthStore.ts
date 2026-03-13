import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { AuthUserResponse, TokenResponse } from "@/types/auth"

interface OriginalSession {
  tokens: TokenResponse
  user: AuthUserResponse
}

interface AuthState {
  tokens: TokenResponse | null
  user: AuthUserResponse | null
  isAuthenticated: boolean
  originalSession: OriginalSession | null
  isImpersonating: boolean
}

interface AuthActions {
  setAuth: (tokens: TokenResponse, user: AuthUserResponse) => void
  setUser: (user: AuthUserResponse) => void
  setTokens: (tokens: TokenResponse) => void
  logout: () => void
  startImpersonation: (tokens: TokenResponse, user: AuthUserResponse) => void
  stopImpersonation: () => void
}

export const useAuthStore = create<AuthState & AuthActions>()(
  persist(
    (set) => ({
      tokens: null,
      user: null,
      isAuthenticated: false,
      originalSession: null,
      isImpersonating: false,

      setAuth: (tokens, user) =>
        set({ tokens, user, isAuthenticated: true }),

      setUser: (user) =>
        set({ user }),

      setTokens: (tokens) =>
        set({ tokens }),

      logout: () =>
        set({ tokens: null, user: null, isAuthenticated: false, originalSession: null, isImpersonating: false }),

      startImpersonation: (tokens, user) =>
        set((state) => ({
          originalSession: state.tokens && state.user
            ? { tokens: state.tokens, user: state.user }
            : state.originalSession,
          tokens,
          user,
          isAuthenticated: true,
          isImpersonating: true,
        })),

      stopImpersonation: () =>
        set((state) => {
          if (!state.originalSession) return state
          return {
            tokens: state.originalSession.tokens,
            user: state.originalSession.user,
            isAuthenticated: true,
            originalSession: null,
            isImpersonating: false,
          }
        }),
    }),
    {
      name: "metapus-auth",
      partialize: (state) => ({
        tokens: state.tokens,
        user: state.user,
        isAuthenticated: state.isAuthenticated,
        originalSession: state.originalSession,
        isImpersonating: state.isImpersonating,
      }),
    }
  )
)

if (typeof window !== "undefined") {
  window.addEventListener("storage", (e) => {
    if (e.key === "metapus-auth" && e.newValue) {
      try {
        const stored = JSON.parse(e.newValue)
        if (stored?.state) {
          useAuthStore.setState(stored.state)
        }
      } catch (err) {
        // ignore parsing errors
      }
    } else if (e.key === "metapus-auth" && !e.newValue) {
      useAuthStore.getState().logout()
    }
  })
}

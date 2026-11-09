import { create } from "zustand"
import { persist } from "zustand/middleware"
import { z } from "zod"
import { getSessionGeneration, scopeKey, transitionScope } from "@/lib/auth-scope"
import { authUserSchema, persistedAuthSchema, tokenResponseSchema } from "@/lib/schemas/auth"
import type { AuthCacheScope, AuthUserResponse, TokenResponse } from "@/types/auth"

interface OriginalSession { tokens: TokenResponse; user: AuthUserResponse }
interface AuthState {
  tokens: TokenResponse | null
  user: AuthUserResponse | null
  cacheScope: AuthCacheScope | null
  generation: number
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

export function isAdminPrincipal(user: AuthUserResponse | null | undefined): boolean {
  return Boolean(user && (user.isAdmin || (user.roles ?? []).some((r) => r.code === "admin" || r.code === "security_admin")))
}
export function isSystemAdmin(user: AuthUserResponse | null | undefined): boolean {
  return Boolean(user && (user.isAdmin || (user.roles ?? []).some((r) => r.code === "admin")))
}

const anonymous = { tokens: null, user: null, cacheScope: null, isAuthenticated: false, originalSession: null, isImpersonating: false }

function authenticated(tokens: TokenResponse, user: AuthUserResponse) {
  const checkedTokens = tokenResponseSchema.parse(tokens)
  const checkedUser = authUserSchema.parse(user)
  if (checkedTokens.cacheScope.principalId !== checkedUser.id) throw new Error("Auth scope does not match principal")
  return { tokens: checkedTokens, user: checkedUser, cacheScope: checkedTokens.cacheScope, isAuthenticated: true,
    generation: transitionScope(checkedTokens.cacheScope) }
}

export const useAuthStore = create<AuthState & AuthActions>()(persist((set, get) => ({
  ...anonymous, generation: getSessionGeneration(),
  setAuth: (tokens, user) => set({ ...authenticated(tokens, user), originalSession: null, isImpersonating: false }),
  setUser: (user) => {
    const checked = authUserSchema.parse(user)
    if (checked.id === get().cacheScope?.principalId) set({ user: checked })
  },
  setTokens: (tokens) => {
    const checked = tokenResponseSchema.parse(tokens)
    const current = get()
    // A rotating cookie cannot silently switch an impersonated or cross-tab identity.
    if (!current.cacheScope || checked.cacheScope.principalId !== current.cacheScope.principalId ||
        checked.cacheScope.tenantId !== current.cacheScope.tenantId || checked.cacheScope.sessionId !== current.cacheScope.sessionId) {
      throw new Error("Refreshed session does not match active identity")
    }
    const changed = scopeKey(checked.cacheScope) !== scopeKey(current.cacheScope)
    set({ tokens: checked, cacheScope: checked.cacheScope, user: changed ? null : current.user,
      generation: transitionScope(checked.cacheScope) })
  },
  logout: () => set({ ...anonymous, generation: transitionScope(null) }),
  startImpersonation: (tokens, user) => {
    const current = get()
    const originalSession = current.originalSession ?? (current.tokens && current.user ? { tokens: current.tokens, user: current.user } : null)
    set({ ...authenticated(tokens, user), originalSession, isImpersonating: true })
  },
  stopImpersonation: () => {
    const original = get().originalSession
    if (original) set({ ...authenticated(original.tokens, original.user), originalSession: null, isImpersonating: false })
  },
}), {
  name: "metapus-auth",
  partialize: (state) => state.isImpersonating ? anonymous : {
    tokens: !state.user || isAdminPrincipal(state.user) ? null : state.tokens, user: state.user,
    cacheScope: state.cacheScope, isAuthenticated: state.isAuthenticated,
    originalSession: null, isImpersonating: false,
  },
  merge: (persisted, current) => {
    const parsed = persistedAuthSchema.safeParse(persisted)
    if (!parsed.success || !validSnapshot(parsed.data)) return { ...current, ...anonymous, generation: transitionScope(null) }
    const incoming = parsed.data
    return { ...current, ...incoming, generation: transitionScope(incoming.cacheScope) }
  },
}))

function validSnapshot(state: z.infer<typeof persistedAuthSchema>): boolean {
  if (!state.isAuthenticated) return state.tokens === null && state.user === null && state.cacheScope === null
  return state.cacheScope !== null && (state.user === null || state.user.id === state.cacheScope.principalId) &&
    (state.tokens === null || scopeKey(state.tokens.cacheScope) === scopeKey(state.cacheScope))
}

// storage events are the shared auth coordination channel. Sanitized admin
// snapshots carry their scope even though the access token stays volatile.
if (typeof window !== "undefined") {
  window.addEventListener("storage", (event) => {
    if (event.key !== "metapus-auth") return
    try {
      // Multiple tabs can enqueue events before this page resumes. Apply only
      // the currently stored snapshot, never an already superseded identity.
      if (localStorage.getItem("metapus-auth") !== event.newValue) return
      const parsed = z.object({ state: persistedAuthSchema }).safeParse(event.newValue ? JSON.parse(event.newValue) : null)
      if (!parsed.success || !validSnapshot(parsed.data.state)) {
        useAuthStore.getState().logout()
        return
      }
      const incoming = parsed.data.state
      const current = useAuthStore.getState()
      const sameScope = scopeKey(incoming.cacheScope) === scopeKey(current.cacheScope)
      useAuthStore.setState({ ...incoming, originalSession: null, isImpersonating: false,
        tokens: sameScope && incoming.isAuthenticated && incoming.tokens === null ? current.tokens : incoming.tokens,
        generation: transitionScope(incoming.cacheScope),
      })
    } catch { useAuthStore.getState().logout() }
  })
}

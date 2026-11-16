import type { StoreApi } from "zustand"
import type { AuthCacheScope } from "@/types/auth"

// This module has no dependency on auth, API or application stores. Resets run
// synchronously before the new identity is published to React.
const resets = new Set<() => void>()
let generation = 0
let controller = new AbortController()
const marker = "metapus-cache-scope"
const scopedKeys = new Set(["metapus-tabs", "metapus-recent", "metapus-dashboard"])
// A configuration names its per-user persisted stores with this prefix, so they
// are cleared on an identity change even before their modules have loaded.
export const SCOPED_STORAGE_PREFIX = "metapus-scoped-"

function clearPersistedData() {
  if (typeof localStorage === "undefined") return
  for (const key of Object.keys(localStorage)) {
    if (scopedKeys.has(key) || key.startsWith(SCOPED_STORAGE_PREFIX) || key.startsWith("metapus-last-used-")) localStorage.removeItem(key)
  }
}

function initialKey(): string | null {
  if (typeof localStorage === "undefined") return null
  try {
    const key = localStorage.getItem(marker)
    if (!key) clearPersistedData() // discard caches from the pre-scope contract
    return key
  } catch { return null }
}

let activeKey = initialKey()

export function scopeKey(scope: AuthCacheScope | null): string | null {
  return scope ? JSON.stringify([scope.tenantId, scope.principalId, scope.sessionId, scope.userVersion, scope.policyVersion]) : null
}

export function getSessionGeneration(): number { return generation }
export function isCurrentSession(value: number): boolean { return value === generation }
export function assertCurrentSession(value: number): void {
  if (!isCurrentSession(value)) throw new DOMException("Session scope changed", "AbortError")
}
export function sessionSignal(): AbortSignal { return controller.signal }

export function onScopeReset(reset: () => void): () => void {
  resets.add(reset)
  return () => { resets.delete(reset) }
}

export function registerScopedStore<T>(store: StoreApi<T>, reset?: () => void): void {
  onScopeReset(reset ?? (() => store.setState(store.getInitialState(), true)))
}

export function transitionScope(scope: AuthCacheScope | null): number {
  const key = scopeKey(scope)
  if (key === activeKey) return generation
  generation++
  controller.abort()
  controller = new AbortController()
  // Reset live stores as well as stores whose modules have not been loaded yet.
  let failed = false
  for (const reset of resets) {
    try { reset() } catch { failed = true }
  }
  if (failed) throw new Error("Unable to clear the previous session")
  activeKey = key
  try {
    clearPersistedData()
    if (typeof localStorage !== "undefined") {
      if (key) localStorage.setItem(marker, key)
      else localStorage.removeItem(marker)
    }
  } catch { /* Storage can be disabled; in-memory isolation still applies. */ }
  return generation
}

import { registerScopedStore } from "@/lib/auth-scope"
import { create } from "zustand"
import { createScopedStateRegistry, type ScopedStateKey } from "@/lib/scoped-state"

const registry = createScopedStateRegistry()
/** Declare keys at module scope so they survive component remounts. */
export const createTabStateKey = registry.define
export type TabStateKey<T> = ScopedStateKey<T>

/**
 * Universal tab state cache — persists arbitrary key-value state per tab.
 *
 * Each tab is identified by its pathname (same as tab.id in useTabsStore).
 * When a tab is closed, its cache is cleared by site-header.tsx.
 *
 * Used by:
 *  - useTabState hook (drop-in useState replacement)
 *  - List hooks (useEntityListPage) for caching fetched data
 */

interface TabStateStore {
  cache: Record<string, Record<symbol, boolean>>
  set: <T>(tabId: string, key: TabStateKey<T>, value: NoInfer<T>) => void
  get: <T>(tabId: string, key: TabStateKey<T>) => T | undefined
  clearTab: (tabId: string) => void
}

export const useTabStateStore = create<TabStateStore>((set, get) => ({
  cache: {},

  set: (tabId, key, value) => {
    key.write(tabId, value)
    set((state) => ({
      cache: {
        ...state.cache,
        [tabId]: { ...(state.cache[tabId] ?? {}), [key.id]: value !== undefined },
      },
    }))
  },

  get: (tabId, key) => get().cache[tabId]?.[key.id] ? key.read(tabId) : undefined,

  clearTab: (tabId) => {
    registry.clear(tabId)
    set((state) => {
      const { [tabId]: _, ...rest } = state.cache
      return { cache: rest }
    })
  },
}))

registerScopedStore(useTabStateStore, () => {
  registry.reset()
  useTabStateStore.setState(useTabStateStore.getInitialState(), true)
})

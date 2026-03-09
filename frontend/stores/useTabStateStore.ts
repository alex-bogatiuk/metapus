import { create } from "zustand"

/**
 * Universal tab state cache — persists arbitrary key-value state per tab.
 *
 * Each tab is identified by its pathname (same as tab.id in useTabsStore).
 * When a tab is closed, its cache is cleared by site-header.tsx.
 *
 * Used by:
 *  - useTabState hook (drop-in useState replacement)
 *  - List hooks (useListPage, useDocumentListPage) for caching fetched data
 */

interface TabStateStore {
  cache: Record<string, Record<string, unknown>>
  set: (tabId: string, key: string, value: unknown) => void
  get: (tabId: string, key: string) => unknown | undefined
  clearTab: (tabId: string) => void
}

export const useTabStateStore = create<TabStateStore>((set, get) => ({
  cache: {},

  set: (tabId, key, value) =>
    set((state) => ({
      cache: {
        ...state.cache,
        [tabId]: { ...(state.cache[tabId] ?? {}), [key]: value },
      },
    })),

  get: (tabId, key) => get().cache[tabId]?.[key],

  clearTab: (tabId) =>
    set((state) => {
      const { [tabId]: _, ...rest } = state.cache
      return { cache: rest }
    }),
}))

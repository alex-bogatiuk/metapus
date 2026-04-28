// frontend/stores/useRecentStore.ts
/**
 * Recent Documents store — tracks recently closed/visited tabs.
 *
 * Pure client-side store with localStorage persistence.
 * No backend API needed — built on top of tab lifecycle.
 *
 * Recording happens in two places:
 *   1. useCloseTab hook — when a tab is closed
 *   2. SiteHeader — when a tab is activated (updates visitedAt)
 */

import { create } from "zustand"
import { persist } from "zustand/middleware"
import { parseEntityTypeFromUrl } from "@/lib/entity-url"

const _maxRecentItems = 20

export interface RecentItem {
  /** Canonical URL — used as unique key (matches tab.id). */
  url: string
  /** Cached tab title at time of recording. */
  title: string
  /** Entity type key for icon resolution (e.g. "counterparty", "goods_receipt"). */
  entityType?: string
  /** ISO 8601 timestamp of last visit. */
  visitedAt: string
}

interface RecentState {
  items: RecentItem[]

  /** Record a recent visit. Deduplicates by URL, caps at _maxRecentItems. */
  addRecent: (entry: { url: string; title: string }) => void
  /** Remove a single item by URL. */
  removeRecent: (url: string) => void
  /** Clear all recent items. */
  clearAll: () => void
}

export const useRecentStore = create<RecentState>()(
  persist(
    (set, get) => ({
      items: [],

      addRecent: ({ url, title }) => {
        // Skip home page — it's always available
        if (url === "/") return

        const { items } = get()
        const entityType = parseEntityTypeFromUrl(url)
        const now = new Date().toISOString()

        const newItem: RecentItem = { url, title, entityType, visitedAt: now }

        // Remove existing entry with same URL (dedup), prepend new, cap
        const filtered = items.filter((item) => item.url !== url)
        const next = [newItem, ...filtered].slice(0, _maxRecentItems)

        set({ items: next })
      },

      removeRecent: (url) => {
        set({ items: get().items.filter((item) => item.url !== url) })
      },

      clearAll: () => {
        set({ items: [] })
      },
    }),
    {
      name: "metapus-recent",
      // Only persist items array
      partialize: (state) => ({ items: state.items }),
    },
  ),
)

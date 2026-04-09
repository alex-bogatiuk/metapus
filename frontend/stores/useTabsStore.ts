import { create } from "zustand"
import { persist } from "zustand/middleware"

export interface Tab {
  id: string
  title: string
  url: string
  isDirty?: boolean
}

export interface CloseTabResult {
  closedId: string
  /** Tab id to navigate to, or null if the closed tab was not active. */
  navigateTo: string | null
}

export interface OpenTabResult {
  opened: boolean
  /** Warning message when too many tabs are open (soft limit). */
  warning?: string
}

const TAB_SOFT_LIMIT = 30

interface TabsState {
  tabs: Tab[]
  activeTabId: string
  activationHistory: string[]
  _hasHydrated: boolean

  /** Opens a tab. If a tab with the same id already exists, activates it instead. */
  openTab: (tab: Tab) => OpenTabResult
  /** Sets the active tab by id. */
  setActiveTab: (id: string) => void
  /** Closes a tab by id. Returns close result with navigation info, or null if nothing closed. */
  closeTab: (id: string) => CloseTabResult | null
  /** Close all tabs except keepId. Returns array of closed tab ids. */
  closeOtherTabs: (keepId: string) => string[]
  /** Close all tabs to the right of the given id. Returns array of closed tab ids. */
  closeTabsToRight: (id: string) => string[]
  /** Close all tabs (reset to default). Returns array of closed tab ids. */
  closeAllTabs: () => string[]
  /** Marks a tab as dirty (unsaved changes) or clean. */
  setTabDirty: (id: string, isDirty: boolean) => void
  /** Updates the title of a tab (e.g. after loading entity data). */
  updateTabTitle: (id: string, title: string) => void
  /** Updates the URL of a tab (e.g. when search params change). */
  updateTabUrl: (id: string, url: string) => void
}

export const DEFAULT_TAB: Tab = {
  id: "/",
  title: "Главное",
  url: "/",
}

/** Push id to front of activation history stack (dedupe, capped). */
function pushHistory(history: string[], id: string): string[] {
  return [id, ...history.filter((h) => h !== id)].slice(0, 20)
}

/** Find the best next active tab from activation history, falling back to index-based. */
function resolveNextActive(
  remaining: Tab[],
  history: string[],
  closedIndex: number,
): string {
  // Try activation history first
  const fromHistory = history.find((h) => remaining.some((t) => t.id === h))
  if (fromHistory) return fromHistory
  // Fallback: nearest by index
  const newIndex = Math.min(closedIndex, remaining.length - 1)
  return remaining[newIndex].id
}

export const useTabsStore = create<TabsState>()(
  persist(
    (set, get) => ({
      tabs: [DEFAULT_TAB],
      activeTabId: DEFAULT_TAB.id,
      activationHistory: [DEFAULT_TAB.id],
      _hasHydrated: false,

      openTab: (tab) => {
        const { tabs, activationHistory } = get()
        const existing = tabs.find((t) => t.id === tab.id)
        let warning: string | undefined

        if (existing) {
          set({
            activeTabId: existing.id,
            activationHistory: pushHistory(activationHistory, existing.id),
          })
          return { opened: false }
        }

        if (tabs.length >= TAB_SOFT_LIMIT) {
          warning = `Открыто много вкладок (${tabs.length}). Закройте неиспользуемые.`
        }

        set({
          tabs: [...tabs, tab],
          activeTabId: tab.id,
          activationHistory: pushHistory(activationHistory, tab.id),
        })
        return { opened: true, warning }
      },

      setActiveTab: (id) => {
        const { activationHistory } = get()
        set({
          activeTabId: id,
          activationHistory: pushHistory(activationHistory, id),
        })
      },

      closeTab: (id) => {
        const { tabs, activeTabId, activationHistory } = get()
        if (tabs.length <= 1) return null // never close the last tab

        const closedIndex = tabs.findIndex((t) => t.id === id)
        if (closedIndex === -1) return null

        const remaining = tabs.filter((t) => t.id !== id)
        const newHistory = activationHistory.filter((h) => h !== id)

        let navigateTo: string | null = null
        let newActiveId = activeTabId

        if (id === activeTabId) {
          newActiveId = resolveNextActive(remaining, newHistory, closedIndex)
          navigateTo = newActiveId
        }

        set({
          tabs: remaining,
          activeTabId: newActiveId,
          activationHistory: newHistory,
        })

        return { closedId: id, navigateTo }
      },

      closeOtherTabs: (keepId) => {
        const { tabs, activationHistory } = get()
        const kept = tabs.filter((t) => t.id === keepId)
        if (kept.length === 0) return [] // keepId not found

        const closedIds = tabs.filter((t) => t.id !== keepId).map((t) => t.id)
        const newHistory = activationHistory.filter((h) => h === keepId)

        set({
          tabs: kept,
          activeTabId: keepId,
          activationHistory: pushHistory(newHistory, keepId),
        })
        return closedIds
      },

      closeTabsToRight: (id) => {
        const { tabs, activeTabId, activationHistory } = get()
        const idx = tabs.findIndex((t) => t.id === id)
        if (idx === -1) return []

        const kept = tabs.slice(0, idx + 1)
        const closedIds = tabs.slice(idx + 1).map((t) => t.id)
        if (closedIds.length === 0) return []

        const closedSet = new Set(closedIds)
        const newHistory = activationHistory.filter((h) => !closedSet.has(h))

        // If active tab was closed, resolve new active
        let newActiveId = activeTabId
        if (closedSet.has(activeTabId)) {
          newActiveId = resolveNextActive(kept, newHistory, kept.length - 1)
        }

        set({
          tabs: kept,
          activeTabId: newActiveId,
          activationHistory: newHistory,
        })
        return closedIds
      },

      closeAllTabs: () => {
        const { tabs } = get()
        const closedIds = tabs.filter((t) => t.id !== DEFAULT_TAB.id).map((t) => t.id)

        set({
          tabs: [DEFAULT_TAB],
          activeTabId: DEFAULT_TAB.id,
          activationHistory: [DEFAULT_TAB.id],
        })
        return closedIds
      },

      setTabDirty: (id, isDirty) => {
        set((state) => ({
          tabs: state.tabs.map((t) => (t.id === id ? { ...t, isDirty } : t)),
        }))
      },

      updateTabTitle: (id, title) => {
        set((state) => ({
          tabs: state.tabs.map((t) => (t.id === id ? { ...t, title } : t)),
        }))
      },

      updateTabUrl: (id, url) => {
        set((state) => ({
          tabs: state.tabs.map((t) => (t.id === id ? { ...t, url } : t)),
        }))
      },
    }),
    {
      name: "metapus-tabs",
      skipHydration: true,
      partialize: (state) => ({
        tabs: state.tabs.map(({ isDirty, ...t }) => t),
        activeTabId: state.activeTabId,
        activationHistory: state.activationHistory,
      }),
      onRehydrateStorage: () => () => {
        useTabsStore.setState({ _hasHydrated: true })
      },
    },
  ),
)

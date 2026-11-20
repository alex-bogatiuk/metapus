import { registerScopedStore } from "@/lib/auth-scope"
import { create } from "zustand"
import { persist } from "zustand/middleware"

export interface Tab {
  id: string
  title: string
  url: string
  isDirty?: boolean
  /** The title was guessed from the URL and gives way to a better one. */
  provisional?: boolean
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
  closingTabIds: string[]
  _hasHydrated: boolean

  /** Opens a tab. If a tab with the same id already exists, activates it instead. */
  openTab: (tab: Tab) => OpenTabResult
  /** Sets the active tab by id. */
  setActiveTab: (id: string) => void
  /** Marks tabs as closing for exit animation, picks next active tab if needed. */
  startClosingTabs: (ids: string[]) => { closedIds: string[]; navigateTo: string | null } | null
  /** Finalizes removal of closing tabs from state after animation completes. */
  finalizeCloseTabs: (ids: string[]) => void
  /** Closes a tab by id immediately without animation. Returns close result with navigation info, or null if nothing closed. */
  closeTab: (id: string) => CloseTabResult | null
  /** Reorders tabs by moving activeId to overId position. Returns true when order changed. */
  reorderTab: (activeId: string, overId: string) => boolean
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
  /** Re-resolves the titles of tabs whose title was guessed from their URL. */
  refreshProvisionalTitles: (resolve: (pathname: string) => string) => void
}

export const DEFAULT_TAB: Tab = {
  id: "/",
  title: "Главное",
  url: "/",
}

const UUID_TITLE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/**
 * Whether a tab's title is a guess from its URL. Tabs saved before the flag
 * existed are recognised by the shapes such a guess takes: a trailing ellipsis
 * or a raw id.
 */
function isGuessedTitle(tab: Tab): boolean {
  return Boolean(tab.provisional) || tab.title.endsWith("…") || UUID_TITLE.test(tab.title)
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

function moveTab(tabs: Tab[], fromIndex: number, toIndex: number): Tab[] {
  const next = tabs.slice()
  const [moved] = next.splice(fromIndex, 1)
  next.splice(toIndex, 0, moved)
  return next
}

export const useTabsStore = create<TabsState>()(
  persist(
    (set, get) => ({
      tabs: [DEFAULT_TAB],
      activeTabId: DEFAULT_TAB.id,
      activationHistory: [DEFAULT_TAB.id],
      closingTabIds: [],
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

      startClosingTabs: (ids) => {
        const { tabs, activeTabId, activationHistory, closingTabIds } = get()
        const validIds = ids.filter(
          (id) => tabs.some((t) => t.id === id) && !closingTabIds.includes(id),
        )
        if (validIds.length === 0) return null

        // Ensure at least one tab remains open
        const surviving = tabs.filter(
          (t) => !closingTabIds.includes(t.id) && !validIds.includes(t.id),
        )
        if (surviving.length === 0) return null

        const newHistory = activationHistory.filter((h) => !validIds.includes(h))
        let navigateTo: string | null = null
        let newActiveId = activeTabId

        if (validIds.includes(activeTabId)) {
          const closedIndex = tabs.findIndex((t) => t.id === activeTabId)
          newActiveId = resolveNextActive(surviving, newHistory, closedIndex)
          navigateTo = newActiveId
        }

        set({
          closingTabIds: [...closingTabIds, ...validIds],
          activeTabId: newActiveId,
          activationHistory: newHistory,
        })

        return { closedIds: validIds, navigateTo }
      },

      finalizeCloseTabs: (ids) => {
        const { tabs, closingTabIds } = get()
        set({
          tabs: tabs.filter((t) => !ids.includes(t.id)),
          closingTabIds: closingTabIds.filter((id) => !ids.includes(id)),
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

      reorderTab: (activeId, overId) => {
        if (activeId === overId) return false

        const { tabs } = get()
        const fromIndex = tabs.findIndex((t) => t.id === activeId)
        const toIndex = tabs.findIndex((t) => t.id === overId)

        if (fromIndex === -1 || toIndex === -1 || fromIndex === toIndex) {
          return false
        }

        set({ tabs: moveTab(tabs, fromIndex, toIndex) })
        return true
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
          tabs: state.tabs.map((t) => (t.id === id ? { ...t, title, provisional: undefined } : t)),
        }))
      },

      refreshProvisionalTitles: (resolve) => {
        const { tabs } = get()
        let changed = false
        const next = tabs.map((t) => {
          if (!isGuessedTitle(t)) return t
          const title = resolve(t.id)
          if (title === t.title) return t
          changed = true
          return { ...t, title }
        })
        if (changed) set({ tabs: next })
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

registerScopedStore(useTabsStore, () => useTabsStore.setState({ ...useTabsStore.getInitialState(), _hasHydrated: true }, true))

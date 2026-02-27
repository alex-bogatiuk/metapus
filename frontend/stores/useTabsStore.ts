import { create } from "zustand"

export interface Tab {
  id: string
  title: string
  url: string
  isDirty?: boolean
}

interface TabsState {
  tabs: Tab[]
  activeTabId: string

  /** Opens a tab. If a tab with the same id already exists, activates it instead. */
  openTab: (tab: Tab) => void
  /** Sets the active tab by id. */
  setActiveTab: (id: string) => void
  /** Closes a tab by id. If it was the active tab, activates the nearest sibling. */
  closeTab: (id: string) => void
  /** Marks a tab as dirty (unsaved changes) or clean. */
  setTabDirty: (id: string, isDirty: boolean) => void
  /** Updates the title of a tab (e.g. after loading entity data). */
  updateTabTitle: (id: string, title: string) => void
}

const DEFAULT_TAB: Tab = {
  id: "/",
  title: "Главное",
  url: "/",
}

export const useTabsStore = create<TabsState>((set, get) => ({
  tabs: [DEFAULT_TAB],
  activeTabId: DEFAULT_TAB.id,

  openTab: (tab) => {
    const { tabs } = get()
    const existing = tabs.find((t) => t.id === tab.id)
    if (existing) {
      set({ activeTabId: existing.id })
    } else {
      set({ tabs: [...tabs, tab], activeTabId: tab.id })
    }
  },

  setActiveTab: (id) => {
    set({ activeTabId: id })
  },

  closeTab: (id) => {
    const { tabs, activeTabId } = get()
    if (tabs.length <= 1) return // never close the last tab

    const closedIndex = tabs.findIndex((t) => t.id === id)
    // Strip dirty flag on removal — the tab is gone
    const remaining = tabs
      .filter((t) => t.id !== id)
      .map((t) => (t.id === id ? { ...t, isDirty: false } : t))

    let newActiveId = activeTabId
    if (id === activeTabId) {
      const newIndex = Math.min(closedIndex, remaining.length - 1)
      newActiveId = remaining[newIndex].id
    }

    set({ tabs: remaining, activeTabId: newActiveId })
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
}))

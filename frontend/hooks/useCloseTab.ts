import { useCallback } from "react"
import { useRouter } from "next/navigation"
import { useTabsStore } from "@/stores/useTabsStore"
import { useFormDraftStore } from "@/stores/useFormDraftStore"
import { useTabStateStore } from "@/stores/useTabStateStore"

/**
 * Orchestrates tab closing across three stores:
 *  - useTabsStore (tab state)
 *  - useFormDraftStore (form drafts in localStorage)
 *  - useTabStateStore (per-tab UI state cache)
 *
 * Also handles navigation after closing the active tab.
 */
export function useCloseTab() {
  const router = useRouter()
  const clearDraft = useFormDraftStore((s) => s.clearDraft)
  const clearTabState = useTabStateStore((s) => s.clearTab)

  const cleanupClosedTabs = useCallback(
    (closedIds: string[]) => {
      for (const id of closedIds) {
        clearDraft(id)
        clearTabState(id)
      }
    },
    [clearDraft, clearTabState],
  )

  const navigateAfterClose = useCallback(
    (newActiveTabId: string | null) => {
      if (newActiveTabId) {
        const tab = useTabsStore.getState().tabs.find((t) => t.id === newActiveTabId)
        if (tab) router.push(tab.url)
      }
    },
    [router],
  )

  /** Close a single tab. Returns true if the tab was closed (not dirty-blocked). */
  const closeOne = useCallback(
    (id: string) => {
      const result = useTabsStore.getState().closeTab(id)
      if (!result) return // last tab, nothing closed
      cleanupClosedTabs([result.closedId])
      navigateAfterClose(result.navigateTo)
    },
    [cleanupClosedTabs, navigateAfterClose],
  )

  /** Close all tabs except the one with keepId. Returns closed tab ids. */
  const closeOthers = useCallback(
    (keepId: string) => {
      const closedIds = useTabsStore.getState().closeOtherTabs(keepId)
      cleanupClosedTabs(closedIds)
      // Active tab is now keepId — navigate to it
      const tab = useTabsStore.getState().tabs.find((t) => t.id === keepId)
      if (tab) router.push(tab.url)
      return closedIds
    },
    [cleanupClosedTabs, router],
  )

  /** Close all tabs to the right of the given id. Returns closed tab ids. */
  const closeRight = useCallback(
    (id: string) => {
      const closedIds = useTabsStore.getState().closeTabsToRight(id)
      cleanupClosedTabs(closedIds)
      // If active tab was closed, store already updated activeTabId
      const { activeTabId, tabs } = useTabsStore.getState()
      const activeTab = tabs.find((t) => t.id === activeTabId)
      if (activeTab) router.push(activeTab.url)
      return closedIds
    },
    [cleanupClosedTabs, router],
  )

  /** Close all tabs (reset to default). Returns closed tab ids. */
  const closeAll = useCallback(() => {
    const closedIds = useTabsStore.getState().closeAllTabs()
    cleanupClosedTabs(closedIds)
    router.push("/")
    return closedIds
  }, [cleanupClosedTabs, router])

  return { closeOne, closeOthers, closeRight, closeAll }
}

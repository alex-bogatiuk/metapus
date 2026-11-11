import { useCallback } from "react"
import { useRouter } from "next/navigation"
import { useTabsStore, type Tab } from "@/stores/useTabsStore"
import { useRecentStore } from "@/stores/useRecentStore"
import { useFormDraftStore } from "@/stores/useFormDraftStore"
import { useTabStateStore } from "@/stores/useTabStateStore"
import { resolveTitleFromUrl } from "@/lib/tab-utils"

const TAB_CLOSE_ANIMATION_MS = 180

/**
 * Orchestrates tab closing across stores:
 *  - useTabsStore (tab state & closing animations)
 *  - useFormDraftStore (form drafts in localStorage)
 *  - useTabStateStore (per-tab UI state cache)
 *
 * Implements smooth Apple-style closing transitions:
 *  1. Tabs are marked closing and animate out (shrink, scale, fade out).
 *  2. Active tab navigation starts immediately.
 *  3. After 180ms, tabs are removed from state and cache is cleaned up.
 */
export function useCloseTab() {
  const router = useRouter()
  const clearDraft = useFormDraftStore((s) => s.clearDraft)
  const clearTabState = useTabStateStore((s) => s.clearTab)

  /**
   * Record closed tabs as recent and clean up associated state.
   * @param closedIds - IDs of tabs that were closed
   * @param tabSnapshot - pre-mutation snapshot of tabs
   */
  const cleanupClosedTabs = useCallback(
    (closedIds: string[], tabSnapshot: Tab[]) => {
      for (const id of closedIds) {
        // Record as recent using pre-mutation snapshot
        const tab = tabSnapshot.find((t) => t.id === id)
        if (tab && tab.id !== "/") {
          useRecentStore.getState().addRecent({
            url: tab.url,
            title: tab.title,
          })
        }
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

  /** Close a single tab with Apple-style fluid collapse and exit animation. */
  const closeOne = useCallback(
    (id: string) => {
      const tabSnapshot = useTabsStore.getState().tabs
      const result = useTabsStore.getState().startClosingTabs([id])
      if (!result) return false // cannot close (last tab or already closing)

      // Immediately navigate to the target tab so the new view starts rendering
      navigateAfterClose(result.navigateTo)

      // Allow 180ms for the Apple-style exit animation to complete before removing from DOM & store
      setTimeout(() => {
        useTabsStore.getState().finalizeCloseTabs([id])
        cleanupClosedTabs([id], tabSnapshot)
      }, TAB_CLOSE_ANIMATION_MS)

      return true
    },
    [cleanupClosedTabs, navigateAfterClose],
  )

  /**
   * Activate the list tab with its own sort and filters and ask it to select
   * the record: the list loads the window around it when the record passes
   * the list's filters.
   */
  const activateListAround = useCallback((listPath: string, recordId: string) => {
    const store = useTabsStore.getState()
    const listTab = store.tabs.find((tab) => tab.id === listPath)
    const target = new URL(listTab?.url ?? listPath, window.location.origin)
    target.searchParams.set("around", recordId)
    const url = `${target.pathname}${target.search}${target.hash}`

    store.updateTabUrl(listPath, url)
    store.openTab({ id: listPath, url, title: listTab?.title ?? resolveTitleFromUrl(listPath) })
    return url
  }, [])

  /** Go to the list from an open form and select its record there. */
  const showInList = useCallback(
    (listPath: string, recordId: string) => {
      router.push(activateListAround(listPath, recordId))
    },
    [activateListAround, router],
  )

  /** Close a saved form and explicitly reveal its record in the list tab. */
  const closeAndShowInList = useCallback(
    (id: string, listPath: string, savedId: string) => {
      const url = activateListAround(listPath, savedId)
      // Activate the destination first: closing the form must not navigate via MRU history.
      closeOne(id)
      router.push(url)
    },
    [activateListAround, closeOne, router],
  )

  /** Close all tabs except keepId with smooth Apple animation. */
  const closeOthers = useCallback(
    (keepId: string) => {
      const tabSnapshot = useTabsStore.getState().tabs
      const idsToClose = tabSnapshot
        .filter((t) => t.id !== keepId)
        .map((t) => t.id)
      const result = useTabsStore.getState().startClosingTabs(idsToClose)
      if (!result) return []

      const tab = tabSnapshot.find((t) => t.id === keepId)
      if (tab) router.push(tab.url)

      setTimeout(() => {
        useTabsStore.getState().finalizeCloseTabs(idsToClose)
        cleanupClosedTabs(idsToClose, tabSnapshot)
      }, TAB_CLOSE_ANIMATION_MS)

      return idsToClose
    },
    [cleanupClosedTabs, router],
  )

  /** Close all tabs to the right of the given id with smooth Apple animation. */
  const closeRight = useCallback(
    (id: string) => {
      const tabSnapshot = useTabsStore.getState().tabs
      const idx = tabSnapshot.findIndex((t) => t.id === id)
      if (idx === -1) return []
      const idsToClose = tabSnapshot.slice(idx + 1).map((t) => t.id)
      const result = useTabsStore.getState().startClosingTabs(idsToClose)
      if (!result) return []

      navigateAfterClose(result.navigateTo)

      setTimeout(() => {
        useTabsStore.getState().finalizeCloseTabs(idsToClose)
        cleanupClosedTabs(idsToClose, tabSnapshot)
      }, TAB_CLOSE_ANIMATION_MS)

      return idsToClose
    },
    [cleanupClosedTabs, navigateAfterClose],
  )

  /** Close all tabs (reset to default) with smooth Apple animation. */
  const closeAll = useCallback(() => {
    const tabSnapshot = useTabsStore.getState().tabs
    const idsToClose = tabSnapshot
      .filter((t) => t.id !== "/")
      .map((t) => t.id)
    const result = useTabsStore.getState().startClosingTabs(idsToClose)
    if (!result) return []

    router.push("/")

    setTimeout(() => {
      useTabsStore.getState().finalizeCloseTabs(idsToClose)
      cleanupClosedTabs(idsToClose, tabSnapshot)
    }, TAB_CLOSE_ANIMATION_MS)

    return idsToClose
  }, [cleanupClosedTabs, router])

  return { closeOne, showInList, closeAndShowInList, closeOthers, closeRight, closeAll }
}

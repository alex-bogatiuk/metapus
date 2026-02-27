import { useCallback } from "react"
import { usePathname } from "next/navigation"
import { useTabsStore } from "@/stores/useTabsStore"

/**
 * Hook to register a form/page as "dirty" (having unsaved changes).
 *
 * Usage in a form component:
 * ```tsx
 * const { markDirty, markClean } = useTabDirty()
 *
 * // Call markDirty() when form values change
 * // Call markClean() after successful save
 * ```
 *
 * NOTE: dirty state is NOT cleared on unmount — this is intentional.
 * When the user switches tabs, the form component unmounts, but the
 * dirty flag must persist in the store so the close-confirmation
 * dialog in SiteHeader can still fire.
 * Dirty state is cleared only explicitly via markClean() (e.g. after save)
 * or when the tab is actually closed via closeTab().
 */
export function useTabDirty() {
    const pathname = usePathname()
    const { setTabDirty, tabs } = useTabsStore()

    // Find the tab that matches the current pathname
    const currentTab = tabs.find((t) => t.url === pathname)
    const tabId = currentTab?.id

    const markDirty = useCallback(() => {
        if (tabId) {
            setTabDirty(tabId, true)
        }
    }, [tabId, setTabDirty])

    const markClean = useCallback(() => {
        if (tabId) {
            setTabDirty(tabId, false)
        }
    }, [tabId, setTabDirty])

    return { markDirty, markClean, isDirty: currentTab?.isDirty ?? false }
}

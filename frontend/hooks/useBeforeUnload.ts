import { useEffect } from "react"

/**
 * Prevents accidental page close/refresh when there are unsaved changes.
 *
 * Shows the browser's native "Leave site?" confirmation dialog when
 * the user tries to close the tab, navigate away, or refresh (F5).
 *
 * Usage (centralized — one place for all forms):
 * ```tsx
 * const hasDirtyTabs = tabs.some(t => t.isDirty)
 * useBeforeUnload(hasDirtyTabs)
 * ```
 */
export function useBeforeUnload(active: boolean): void {
  useEffect(() => {
    if (!active) return

    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault()
    }

    window.addEventListener("beforeunload", handler)
    return () => window.removeEventListener("beforeunload", handler)
  }, [active])
}

import { useCallback } from "react"
import { usePathname } from "next/navigation"
import { useFormDraft } from "./useFormDraft"
import { useTabDirty } from "./useTabDirty"
import { useCloseTab } from "./useCloseTab"
import { useTabStateStore } from "@/stores/useTabStateStore"

/**
 * Composable hook for document edit forms.
 *
 * Combines `useFormDraft` (draft persistence) + `useTabDirty` (dirty indicator)
 * into a single orchestration layer with an atomic `syncFromServer` method that
 * **always** pairs `replace(mapDocToState(response))` with `markClean()`.
 *
 * This eliminates an entire class of bugs where a developer forgets to call
 * `markClean()` after refreshing form state from the server (e.g. after post,
 * unpost, toggleDeletionMark, etc.).
 *
 * Usage:
 * ```tsx
 * const {
 *   f, doc, update, syncFromServer, markDirty,
 *   clear, hasDraft, closeAndCleanup,
 * } = useDocumentFormSync<GoodsReceiptEditFormState, GoodsReceiptResponse>(
 *   INITIAL_EDIT_STATE,
 *   mapDocToState,
 *   "/documents/goods-receipts",
 *   { shouldPersist: (s) => !!(s._doc && s.organizationId) },
 * )
 *
 * // After any successful server mutation:
 * syncFromServer(updated)   // ← atomic: replace + markClean
 *
 * // After field change:
 * update({ supplierId: id }); markDirty()
 * ```
 *
 * @param initial         — default state for a fresh form
 * @param mapDocToState   — pure function: server response → form state
 * @param listPath        — list page path for tab cleanup on close
 * @param persistOptions  — optional shouldPersist predicate
 */
export function useDocumentFormSync<
  TFormState extends { _doc: TResponse | null },
  TResponse,
>(
  initial: TFormState,
  mapDocToState: (d: TResponse) => TFormState,
  listPath: string,
  persistOptions?: { shouldPersist?: (s: TFormState) => boolean },
) {
  const pathname = usePathname()
  const { markDirty, markClean } = useTabDirty()
  const { closeOne } = useCloseTab()

  const { state, update, replace, clear, hasDraft } = useFormDraft<TFormState>(
    pathname,
    initial,
    persistOptions,
  )

  /**
   * Atomic: replace form state from server response + mark clean.
   * Use after **every** successful server mutation (save, post, unpost,
   * toggleDeletionMark, etc.).
   */
  const syncFromServer = useCallback(
    (response: TResponse) => {
      replace(mapDocToState(response))
      markClean()
    },
    [replace, markClean, mapDocToState],
  )

  /**
   * Mark clean + clear draft + close tab.
   * Use in "save and close" / "post and close" flows.
   */
  const closeAndCleanup = useCallback(() => {
    markClean()
    clear()
    useTabStateStore.getState().clearTab(listPath)
    closeOne(pathname)
  }, [markClean, clear, listPath, closeOne, pathname])

  return {
    /** Current typed form state. */
    state,
    /** Shortcut: state._doc (the raw server response, if loaded). */
    doc: state._doc,
    /** Merge partial updates into form state. */
    update,
    /** Full replace (use directly only in initial load useEffect). */
    replace,
    /** Atomic: replace from server response + markClean. */
    syncFromServer,
    /** Mark the tab as dirty (call after user edits). */
    markDirty,
    /** Mark the tab as clean (rarely needed directly — prefer syncFromServer). */
    markClean,
    /** Clear the draft from the store. */
    clear,
    /** Whether a draft existed on mount. */
    hasDraft,
    /** Current pathname. */
    pathname,
    /** Atomic: markClean + clear draft + close tab. */
    closeAndCleanup,
  }
}

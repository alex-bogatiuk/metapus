import { useState, useCallback, useEffect, useRef } from "react"
import { useFormDraftStore } from "@/stores/useFormDraftStore"

/**
 * Generic hook for persisting form state across tab switches.
 *
 * Replaces the pattern of 20+ individual `useState` calls + manual draft
 * save/restore boilerplate. The form works with a single typed state object `T`.
 *
 * Usage:
 * ```tsx
 * const { state, update, replace, clear, hasDraft } = useFormDraft<MyForm>(
 *   pathname,
 *   { name: "", quantity: 0, lines: [] }
 * )
 *
 * // Partial update (auto-persists):
 * update({ name: "new value" })
 *
 * // Full replace (after fetch/copy, auto-persists):
 * replace({ ...mappedServerData })
 *
 * // Clear draft (after successful save):
 * clear()
 * ```
 *
 * @param key       — unique key, typically `usePathname()` result
 * @param initial   — default state for a fresh form
 * @param options   — optional config
 *   - shouldPersist: predicate to skip persisting empty forms
 */
export function useFormDraft<T extends object>(
  key: string,
  initial: T,
  options?: {
    shouldPersist?: (state: T) => boolean
  },
): {
  /** Current form state (typed). */
  state: T
  /** Merge partial updates into the state (auto-persists). Accepts object or functional updater. */
  update: (partialOrFn: Partial<T> | ((prev: T) => Partial<T>)) => void
  /** Replace the entire state (e.g. after server fetch or copy). */
  replace: (full: T) => void
  /** Clear the draft from the store (call after successful save). */
  clear: () => void
  /** Whether a draft existed in the store when the hook mounted. */
  hasDraft: boolean
} {
  const { saveDraft, loadDraft, clearDraft } = useFormDraftStore()

  // Restore from draft on mount, or use initial state
  const [restoredFromDraft] = useState(() => {
    const stored = loadDraft(key)
    return stored !== undefined
  })

  const [state, setState] = useState<T>(() => {
    const stored = loadDraft(key)
    return stored !== undefined ? (stored as T) : initial
  })

  // Track whether we've initialized (skip first persist which is the restore itself)
  const initialized = useRef(false)

  // Auto-persist on every state change (skip the initial mount / restore)
  useEffect(() => {
    if (!initialized.current) {
      initialized.current = true
      return
    }
    if (options?.shouldPersist && !options.shouldPersist(state)) return
    saveDraft(key, state as unknown as Record<string, unknown>)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state])

  const update = useCallback((partialOrFn: Partial<T> | ((prev: T) => Partial<T>)) => {
    setState((prev) => {
      const partial = typeof partialOrFn === 'function' ? partialOrFn(prev) : partialOrFn
      return { ...prev, ...partial }
    })
  }, [])

  const replace = useCallback((full: T) => {
    setState(full)
  }, [])

  const clear = useCallback(() => {
    clearDraft(key)
  }, [clearDraft, key])

  return { state, update, replace, clear, hasDraft: restoredFromDraft }
}

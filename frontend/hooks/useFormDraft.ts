import { getSessionGeneration, isCurrentSession } from "@/lib/auth-scope"
import { useFormDraftStore, type FormDraftKey } from "@/stores/useFormDraftStore"
import { useCallback, useEffect, useRef, useState } from "react"

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
 *   formKey,
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
  formKey: FormDraftKey<T>,
  initial: T,
  options?: {
    shouldPersist?: (state: T) => boolean
  },
): {
  /** Current form state (typed). */
  state: T
  /** Merge partial updates into the state (auto-persists). Accepts object or functional updater. */
  update: (partialOrFn: Partial<T> | ((prev: T) => Partial<T>)) => void
  setField: <K extends keyof T>(field: K, value: T[K] | ((previous: T[K]) => T[K])) => void
  /** Replace the entire state (e.g. after server fetch or copy). */
  replace: (full: T) => void
  /** Clear the draft from the store (call after successful save). */
  clear: () => void
  /** Whether a draft existed in the store when the hook mounted. */
  hasDraft: boolean
} {
  const generation = getSessionGeneration()
  const saveDraft = useFormDraftStore((s) => s.saveDraft)
  const loadDraft = useFormDraftStore((s) => s.loadDraft)
  const clearDraft = useFormDraftStore((s) => s.clearDraft)

  // Restore from draft on mount, or use initial state
  const [restoredFromDraft] = useState(() => {
    const stored = loadDraft(key, formKey)
    return stored !== undefined
  })

  const [state, setState] = useState<T>(() => {
    const stored = loadDraft(key, formKey)
    return stored !== undefined ? stored : initial
  })

  // Keep restored state out of the save cycle when only callback identities change.
  const persistedState = useRef(state)
  const shouldPersist = options?.shouldPersist

  // Auto-persist on every state change (skip the initial mount / restore)
  useEffect(() => {
    if (!isCurrentSession(generation)) return
    if (persistedState.current === state) return
    if (shouldPersist && !shouldPersist(state)) return
    saveDraft(key, formKey, state)
    persistedState.current = state
  }, [state, generation, key, formKey, saveDraft, shouldPersist])

  const update = useCallback((partialOrFn: Partial<T> | ((prev: T) => Partial<T>)) => {
    if (!isCurrentSession(generation)) return
    setState((prev) => {
      const partial = typeof partialOrFn === 'function' ? partialOrFn(prev) : partialOrFn
      return { ...prev, ...partial }
    })
  }, [generation])

  const setField = useCallback(<K extends keyof T>(field: K, value: T[K] | ((previous: T[K]) => T[K])) => {
    if (!isCurrentSession(generation)) return
    setState((previous) => {
      const next = typeof value === "function"
        ? (value as (previous: T[K]) => T[K])(previous[field])
        : value
      return Object.is(previous[field], next) ? previous : { ...previous, [field]: next }
    })
  }, [generation])

  const replace = useCallback((full: T) => {
    if (!isCurrentSession(generation)) return
    setState(full)
  }, [generation])

  const clear = useCallback(() => {
    if (isCurrentSession(generation)) clearDraft(key)
  }, [clearDraft, key, generation])

  return { state, update, setField, replace, clear, hasDraft: restoredFromDraft }
}

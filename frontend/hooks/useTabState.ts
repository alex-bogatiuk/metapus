"use client"

import { useState, useCallback } from "react"
import { getSessionGeneration, isCurrentSession } from "@/lib/auth-scope"
import { usePathname } from "next/navigation"
import { useTabStateStore, type TabStateKey } from "@/stores/useTabStateStore"

type SetStateAction<T> = T | ((prev: T) => T)

/**
 * Drop-in replacement for `useState` that persists values across tab switches.
 *
 * The value is stored in a Zustand store keyed by `pathname + key`.
 * On mount, restores the cached value if it exists; otherwise uses `defaultValue`.
 * On every `setValue` call, updates both local React state and the store.
 *
 * When the tab is closed, the cache is cleared by site-header.tsx → clearTab().
 *
 * Usage:
 * ```tsx
 * // Instead of: const [name, setName] = useState("")
 * const [name, setName] = useTabState(nameKey, "")
 * ```
 */
export function useTabState<T>(
  key: TabStateKey<T>,
  defaultValue: NoInfer<T>,
): [T, (v: SetStateAction<T>) => void] {
  const pathname = usePathname()
  const generation = getSessionGeneration()
  const [initial] = useState(() => defaultValue)
  const cached = useTabStateStore(state => state.get(pathname, key))
  const value = cached !== undefined ? cached : initial
  const setValue = useCallback((action: SetStateAction<T>) => {
    if (!isCurrentSession(generation)) return
    const stored = useTabStateStore.getState().get(pathname, key)
    const previous = stored !== undefined ? stored : initial
    const next = typeof action === "function" ? (action as (prev: T) => T)(previous) : action
    useTabStateStore.getState().set(pathname, key, next)
  }, [pathname, key, generation, initial])
  return [value, setValue]
}

/**
 * Returns `true` if the current tab had a cached value for the given key
 * **at mount time**. The value is computed once and never re-evaluated.
 *
 * Uses `getState()` (non-reactive read) instead of a Zustand selector
 * to avoid "Cannot update a component while rendering" errors when
 * `useTabState`'s setValue writes to the store during reconciliation.
 */
export function useHasTabCache(key?: { readonly id: symbol }): boolean {
  const pathname = usePathname()
  const [hasCache] = useState(() => {
    const cache = useTabStateStore.getState().cache
    return key !== undefined
      ? cache[pathname]?.[key.id] === true
      : cache[pathname] !== undefined
  })
  return hasCache
}

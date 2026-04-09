"use client"

import { useState, useCallback, useRef } from "react"
import { usePathname } from "next/navigation"
import { useTabStateStore } from "@/stores/useTabStateStore"

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
 * const [name, setName] = useTabState("name", "")
 * ```
 */
export function useTabState<T>(
  key: string,
  defaultValue: T,
): [T, (v: SetStateAction<T>) => void] {
  const pathname = usePathname()
  const store = useTabStateStore

  const [value, setValueInternal] = useState<T>(() => {
    const cached = store.getState().get(pathname, key)
    return cached !== undefined ? (cached as T) : defaultValue
  })

  const pathnameRef = useRef(pathname)
  pathnameRef.current = pathname

  const setValue = useCallback(
    (action: SetStateAction<T>) => {
      setValueInternal((prev) => {
        const next =
          typeof action === "function"
            ? (action as (prev: T) => T)(prev)
            : action
        store.getState().set(pathnameRef.current, key, next)
        return next
      })
    },
    [key, store],
  )

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
export function useHasTabCache(key?: string): boolean {
  const pathname = usePathname()
  const [hasCache] = useState(() => {
    const cache = useTabStateStore.getState().cache
    return key !== undefined
      ? cache[pathname]?.[key] !== undefined
      : cache[pathname] !== undefined
  })
  return hasCache
}

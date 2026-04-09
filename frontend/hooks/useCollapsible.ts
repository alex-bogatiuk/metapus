"use client"

import { useState, useCallback } from "react"

/**
 * Hook for managing collapsible UI sections with localStorage persistence.
 *
 * Usage:
 * ```tsx
 * const [collapsed, toggle] = useCollapsible("metapus-form-sidebar-collapsed", true)
 * ```
 */
export function useCollapsible(
  storageKey: string,
  defaultValue = false,
): [boolean, () => void] {
  const [collapsed, setCollapsed] = useState(() => {
    if (typeof window === "undefined") return defaultValue
    const stored = localStorage.getItem(storageKey)
    return stored !== null ? stored === "true" : defaultValue
  })

  const toggle = useCallback(() => {
    setCollapsed((prev) => {
      const next = !prev
      localStorage.setItem(storageKey, String(next))
      return next
    })
  }, [storageKey])

  return [collapsed, toggle]
}

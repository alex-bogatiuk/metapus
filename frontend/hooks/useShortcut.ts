// hooks/useShortcut.ts
//
// Declarative hook for registering a keyboard shortcut.
// Registers on mount, unregisters on unmount. Follows Pattern #3 (Custom Hooks as orchestration).

import { useEffect, useRef } from "react"
import { useShortcutStore, type ShortcutGroup } from "@/stores/useShortcutStore"

interface UseShortcutOptions {
  /** Whether the shortcut is currently active. Default: true */
  enabled?: boolean
  /** Priority for conflict resolution. Higher wins. Default: 0 */
  priority?: number
  /** If true, shown in help dialog but not dispatched by ShortcutManager */
  passive?: boolean
}

/**
 * Register a keyboard shortcut declaratively.
 *
 * @example
 * useShortcut("nav.close-tab", "alt+w", "Закрыть вкладку", "navigation", handleClose)
 * useShortcut("list.copy", "f9", "Копировать", "list", handleCopy, { enabled: !!focusedId })
 */
export function useShortcut(
  id: string,
  keys: string,
  label: string,
  group: ShortcutGroup,
  action: () => void,
  options?: UseShortcutOptions,
): void {
  const { enabled = true, priority, passive } = options ?? {}
  const actionRef = useRef(action)
  useEffect(() => {
    actionRef.current = action
  })

  useEffect(() => {
    if (!enabled) {
      // If disabled, make sure it's unregistered
      useShortcutStore.getState().unregister(id)
      return
    }

    useShortcutStore.getState().register({
      id,
      keys,
      label,
      group,
      priority,
      passive,
      action: () => actionRef.current(),
    })

    return () => {
      useShortcutStore.getState().unregister(id)
    }
  }, [id, keys, label, group, enabled, priority, passive])
}

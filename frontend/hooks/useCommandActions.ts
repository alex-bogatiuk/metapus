// frontend/hooks/useCommandActions.ts
/**
 * Declarative hook for registering contextual actions in the Command Palette.
 *
 * Any page or component can call this hook to register actions that appear
 * in the Command Palette (Ctrl+K) when that component is mounted.
 * Actions are automatically cleaned up on unmount.
 *
 * Pattern: same lifecycle as useShortcut (register on mount, unregister on unmount).
 *
 * @example
 * // Inside GoodsReceiptForm.tsx
 * useCommandActions("goods-receipt-form", [
 *   { id: "post", label: "Провести документ", icon: Play, shortcut: ["Ctrl", "Enter"], action: handlePost },
 *   { id: "print", label: "Печать формы", icon: Printer, action: handlePrint },
 * ])
 */

import { useEffect, useRef } from "react"
import {
  useCommandPaletteStore,
  type CommandAction,
} from "@/stores/useCommandPaletteStore"

export function useCommandActions(
  sourceId: string,
  actions: CommandAction[],
): void {
  // Keep a stable ref to avoid unnecessary re-registrations.
  // We still re-register when the actions array identity changes.
  const actionsRef = useRef(actions)

  useEffect(() => {
    actionsRef.current = actions
  })

  useEffect(() => {
    useCommandPaletteStore.getState().registerActions(sourceId, actionsRef.current)

    return () => {
      useCommandPaletteStore.getState().unregisterActions(sourceId)
    }
  }, [sourceId, actions])
}

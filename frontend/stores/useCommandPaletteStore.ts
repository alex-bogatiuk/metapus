// frontend/stores/useCommandPaletteStore.ts
/**
 * Command Palette store — controls visibility and contextual actions.
 *
 * - `isOpen` / `toggle()` / `setOpen()` — dialog state.
 * - `contextActions` — dynamically registered by pages via `useCommandActions` hook.
 *   Actions are keyed by a stable `sourceId` so a page can replace its own set atomically.
 *
 * Pattern: same singleton Zustand approach as useShortcutStore, useTabsStore.
 */

import { create } from "zustand"
import type { LucideIcon } from "lucide-react"

// ── Types ───────────────────────────────────────────────────────────────

export interface CommandAction {
  /** Unique action id (e.g. "goods-receipt:post", "goods-receipt:print") */
  id: string
  /** Human-readable label shown in the palette */
  label: string
  /** Optional Lucide icon component */
  icon?: LucideIcon
  /** Optional keyboard shortcut hint (e.g. ["Ctrl", "Enter"]) */
  shortcut?: string[]
  /** Callback executed on selection */
  action: () => void
  /** Optional group label override (default: "Действия") */
  group?: string
}

interface CommandPaletteState {
  isOpen: boolean
  /**
   * Contextual actions grouped by sourceId.
   * Key = sourceId (e.g. component path), Value = array of actions.
   * This allows atomic registration per source without affecting others.
   */
  actionsBySource: Map<string, CommandAction[]>
  /** Monotonic version for reactive reads (same pattern as useShortcutStore). */
  version: number

  // Actions
  setOpen: (open: boolean) => void
  toggle: () => void
  /**
   * Register a set of contextual actions from a source.
   * Replaces any previous actions from the same sourceId.
   */
  registerActions: (sourceId: string, actions: CommandAction[]) => void
  /** Unregister all actions from a source. */
  unregisterActions: (sourceId: string) => void
  /** Get flattened array of all contextual actions. */
  getAllActions: () => CommandAction[]
}

// ── Store ───────────────────────────────────────────────────────────────

export const useCommandPaletteStore = create<CommandPaletteState>()(
  (set, get) => ({
    isOpen: false,
    actionsBySource: new Map(),
    version: 0,

    setOpen: (open) => set({ isOpen: open }),

    toggle: () => set((s) => ({ isOpen: !s.isOpen })),

    registerActions: (sourceId, actions) => {
      set((state) => {
        const next = new Map(state.actionsBySource)
        next.set(sourceId, actions)
        return { actionsBySource: next, version: state.version + 1 }
      })
    },

    unregisterActions: (sourceId) => {
      set((state) => {
        const next = new Map(state.actionsBySource)
        if (next.delete(sourceId)) {
          return { actionsBySource: next, version: state.version + 1 }
        }
        return state
      })
    },

    getAllActions: () => {
      const result: CommandAction[] = []
      for (const actions of get().actionsBySource.values()) {
        result.push(...actions)
      }
      return result
    },
  }),
)

// stores/useShortcutStore.ts
//
// Zustand store — centralized registry of all active keyboard shortcuts.
// Pattern: singleton state, no prop drilling, same approach as useTabsStore / useMetadataStore.

import { create } from "zustand"

// ── Types ───────────────────────────────────────────────────────────────

export type ShortcutGroup = "navigation" | "editing" | "list" | "general"

export interface ShortcutEntry {
  /** Unique id, e.g. "nav.close-tab", "list.copy" */
  id: string
  /** Normalized combo string: "mod+s", "f9", "alt+w", "mod+enter" */
  keys: string
  /** Human-readable label: "Записать", "Копировать" */
  label: string
  /** Group for help dialog display */
  group: ShortcutGroup
  /** Priority for conflict resolution. Higher = wins. Default: 0 */
  priority: number
  /** Callback to execute when shortcut is triggered */
  action: () => void
  /**
   * If true, shortcut is shown in help dialog but NOT dispatched by ShortcutManager.
   * Used for scoped shortcuts (e.g. ProductPickerDialog onKeyDown).
   */
  passive: boolean
}

export type ShortcutRegistration = Omit<ShortcutEntry, "priority" | "passive"> & {
  priority?: number
  passive?: boolean
}

interface ShortcutState {
  /** Map of id → ShortcutEntry */
  entries: Map<string, ShortcutEntry>
  /** Monotonic version counter — bumped on every register/unregister for reactive reads */
  version: number

  register: (entry: ShortcutRegistration) => void
  unregister: (id: string) => void
  getAll: () => ShortcutEntry[]
  getByGroup: (group: ShortcutGroup) => ShortcutEntry[]
}

// ── Group display order ─────────────────────────────────────────────────

export const GROUP_LABELS: Record<ShortcutGroup, string> = {
  navigation: "Навигация",
  editing: "Редактирование",
  list: "Списки",
  general: "Общие",
}

export const GROUP_ORDER: ShortcutGroup[] = ["navigation", "editing", "list", "general"]

// ── Store ───────────────────────────────────────────────────────────────

export const useShortcutStore = create<ShortcutState>((set, get) => ({
  entries: new Map(),
  version: 0,

  register: (reg) => {
    set((state) => {
      const entries = new Map(state.entries)
      entries.set(reg.id, {
        ...reg,
        priority: reg.priority ?? 0,
        passive: reg.passive ?? false,
      })
      return { entries, version: state.version + 1 }
    })
  },

  unregister: (id) => {
    set((state) => {
      const entries = new Map(state.entries)
      if (entries.delete(id)) {
        return { entries, version: state.version + 1 }
      }
      return state
    })
  },

  getAll: () => {
    return Array.from(get().entries.values())
  },

  getByGroup: (group) => {
    return Array.from(get().entries.values()).filter((e) => e.group === group)
  },
}))

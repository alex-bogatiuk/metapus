import { create } from "zustand"
import type {
  SystemSettings,
  NumberingSettings,
  PerformanceSettings,
  WarehouseSettings,
  SalesSettings,
  PurchasingSettings,
} from "@/types/settings"
import { defaultSystemSettings } from "@/types/settings"
import { api } from "@/lib/api"
import { toast } from "sonner"
import { ApiError } from "@/lib/api"

type SettingsSection = "numbering" | "performance" | "warehouse" | "sales" | "purchasing"

interface SettingsState {
  settings: SystemSettings
  isLoading: boolean
  isSaving: boolean
  error: string | null

  /** Fetch settings from API. */
  fetchSettings: () => Promise<void>

  /** Save a specific section to API with optimistic locking. */
  saveSection: (section: SettingsSection) => Promise<void>

  /** Replaces all settings at once (e.g. after fetch). */
  setSettings: (settings: SystemSettings) => void

  /** Partially updates a section (local state). */
  updateNumbering: (patch: Partial<NumberingSettings>) => void
  updatePerformance: (patch: Partial<PerformanceSettings>) => void
  updateWarehouse: (patch: Partial<WarehouseSettings>) => void
  updateSales: (patch: Partial<SalesSettings>) => void
  updatePurchasing: (patch: Partial<PurchasingSettings>) => void

  /** Generic section updater for metadata-driven renderer. */
  updateSection: (section: SettingsSection, key: string, value: unknown) => void

  setLoading: (v: boolean) => void
  setSaving: (v: boolean) => void
  setError: (v: string | null) => void
}

export type { SettingsSection }

export const useSettingsStore = create<SettingsState>((set, get) => ({
  settings: defaultSystemSettings(),
  isLoading: false,
  isSaving: false,
  error: null,

  fetchSettings: async () => {
    set({ isLoading: true, error: null })
    try {
      const data = await api.settings.get()
      set({ settings: data, isLoading: false })
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Не удалось загрузить настройки. Проверьте соединение или обновите страницу."
      set({ error: msg, isLoading: false })
    }
  },

  saveSection: async (section: SettingsSection) => {
    const state = get()
    set({ isSaving: true })
    try {
      const sectionData = state.settings[section]
      const updated = await api.settings.updateSection(
        section,
        sectionData,
        state.settings.version
      )
      set({ settings: updated, isSaving: false })
      toast.success("Настройки сохранены")
    } catch (err) {
      set({ isSaving: false })
      if (err instanceof ApiError && err.status === 409) {
        toast.error("Настройки изменены другим пользователем. Обновите страницу.")
      } else {
        const msg = err instanceof Error ? err.message : "Не удалось сохранить настройки. Проверьте соединение или попробуйте позже."
        toast.error(msg)
      }
    }
  },

  setSettings: (settings) => set({ settings }),

  updateNumbering: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        numbering: { ...state.settings.numbering, ...patch },
      },
    })),

  updatePerformance: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        performance: { ...state.settings.performance, ...patch },
      },
    })),

  updateWarehouse: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        warehouse: { ...state.settings.warehouse, ...patch },
      },
    })),

  updateSales: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        sales: { ...state.settings.sales, ...patch },
      },
    })),

  updatePurchasing: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        purchasing: { ...state.settings.purchasing, ...patch },
      },
    })),

  /** Generic section updater — used by the metadata-driven renderer. */
  updateSection: (section, key, value) =>
    set((state) => ({
      settings: {
        ...state.settings,
        [section]: {
          ...state.settings[section],
          [key]: value,
        },
      },
    })),

  setLoading: (v) => set({ isLoading: v }),
  setSaving: (v) => set({ isSaving: v }),
  setError: (v) => set({ error: v }),
}))

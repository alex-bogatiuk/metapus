import { create } from "zustand"
import type {
  SystemSettings,
  OrganizationSettings,
  AccountingSettings,
  InterfaceSettings,
} from "@/types/settings"
import { defaultSystemSettings } from "@/types/settings"

interface SettingsState {
  settings: SystemSettings
  isLoading: boolean
  isSaving: boolean
  error: string | null

  /** Replaces all settings at once (e.g. after fetch). */
  setSettings: (settings: SystemSettings) => void

  /** Partially updates organization section. */
  updateOrganization: (patch: Partial<OrganizationSettings>) => void

  /** Partially updates accounting section. */
  updateAccounting: (patch: Partial<AccountingSettings>) => void

  /** Partially updates interface section. */
  updateInterface: (patch: Partial<InterfaceSettings>) => void

  setLoading: (v: boolean) => void
  setSaving: (v: boolean) => void
  setError: (v: string | null) => void
}

export const useSettingsStore = create<SettingsState>((set) => ({
  settings: defaultSystemSettings(),
  isLoading: false,
  isSaving: false,
  error: null,

  setSettings: (settings) => set({ settings }),

  updateOrganization: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        organization: { ...state.settings.organization, ...patch },
      },
    })),

  updateAccounting: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        accounting: { ...state.settings.accounting, ...patch },
      },
    })),

  updateInterface: (patch) =>
    set((state) => ({
      settings: {
        ...state.settings,
        interface: { ...state.settings.interface, ...patch },
      },
    })),

  setLoading: (v) => set({ isLoading: v }),
  setSaving: (v) => set({ isSaving: v }),
  setError: (v) => set({ error: v }),
}))

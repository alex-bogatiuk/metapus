import { create } from "zustand"
import type { ProfilePreset } from "@/components/settings/profile-presets"

interface ProfilePresetStore {
  preset: ProfilePreset | null
  setPreset: (preset: ProfilePreset) => void
  clear: () => void
}

export const useProfilePresetStore = create<ProfilePresetStore>((set) => ({
  preset: null,
  setPreset: (preset) => set({ preset }),
  clear: () => set({ preset: null }),
}))

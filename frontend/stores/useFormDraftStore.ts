import { create } from "zustand"

/**
 * Form draft store — persists form state across tab switches.
 *
 * When a user edits a form and switches tabs, Next.js unmounts the page
 * component, losing all local state. This store acts as a session-level
 * cache: form pages save their state here on every change, and restore
 * it on mount if a draft exists.
 *
 * Drafts are keyed by pathname (same as tab.id in useTabsStore).
 * Cleanup happens when a tab is closed (via clearDraft).
 */

type DraftData = Record<string, unknown>

interface FormDraftState {
  drafts: Record<string, DraftData>
  saveDraft: (key: string, data: DraftData) => void
  loadDraft: (key: string) => DraftData | undefined
  clearDraft: (key: string) => void
}

export const useFormDraftStore = create<FormDraftState>((set, get) => ({
  drafts: {},

  saveDraft: (key, data) =>
    set((state) => ({
      drafts: { ...state.drafts, [key]: data },
    })),

  loadDraft: (key) => get().drafts[key],

  clearDraft: (key) =>
    set((state) => {
      const { [key]: _, ...rest } = state.drafts
      return { drafts: rest }
    }),
}))

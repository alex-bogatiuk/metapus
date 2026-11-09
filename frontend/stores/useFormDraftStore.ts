import { registerScopedStore } from "@/lib/auth-scope"
import { create } from "zustand"
import { createScopedStateRegistry, type ScopedStateKey } from "@/lib/scoped-state"

const registry = createScopedStateRegistry()
export const createFormDraftKey = registry.define
export type FormDraftKey<T> = ScopedStateKey<T>

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

interface FormDraftState {
  drafts: Record<string, Record<symbol, boolean>>
  saveDraft: <T>(path: string, key: FormDraftKey<T>, data: NoInfer<T>) => void
  loadDraft: <T>(path: string, key: FormDraftKey<T>) => T | undefined
  clearDraft: (key: string) => void
}

export const useFormDraftStore = create<FormDraftState>((set, get) => ({
  drafts: {},

  saveDraft: (path, key, data) => {
    key.write(path, data)
    set((state) => ({
      drafts: { ...state.drafts, [path]: { ...state.drafts[path], [key.id]: true } },
    }))
  },

  loadDraft: (path, key) => get().drafts[path]?.[key.id] ? key.read(path) : undefined,

  clearDraft: (key) => {
    registry.clear(key)
    set((state) => {
      const { [key]: _, ...rest } = state.drafts
      return { drafts: rest }
    })
  },
}))

registerScopedStore(useFormDraftStore, () => {
  registry.reset()
  useFormDraftStore.setState(useFormDraftStore.getInitialState(), true)
})

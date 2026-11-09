"use client"

import type { JsonObject } from "@/lib/json-value"
import { registerScopedStore, getSessionGeneration, isCurrentSession } from "@/lib/auth-scope"
import { create } from "zustand"
import { persist } from "zustand/middleware"
import { api } from "@/lib/api"
import { defaultDashboardLayout, normalizeDashboardLayout, persistedDashboardLayout } from "@/lib/dashboard-layout"
import type { DashboardLayout, WidgetPlacement } from "@/types/dashboard"

let saveTimer: ReturnType<typeof setTimeout> | null = null
let saveChain: Promise<void> = Promise.resolve()
function queueSave() {
  if (saveTimer) clearTimeout(saveTimer)
  const generation = getSessionGeneration()
  saveTimer = setTimeout(() => {
    saveTimer = null
    if (isCurrentSession(generation)) void useDashboardStore.getState().saveLayout()
  }, 800)
}
interface DashboardState {
  layout: DashboardLayout
  isEditMode: boolean
  isLoaded: boolean
  isSaving: boolean
  saveError: string | null
  loadError: string | null
  loadLayout: () => Promise<void>
  saveLayout: () => Promise<boolean>
  setEditMode: (mode: boolean) => void
  addWidget: (widget: WidgetPlacement) => void
  removeWidget: (id: string) => void
  updatePositions: (widgets: WidgetPlacement[]) => void
  updateWidgetConfig: (id: string, config: JsonObject) => void
  resetToDefault: () => void
}
export const useDashboardStore = create<DashboardState>()(persist((set, get) => {
  const change = (widgets: WidgetPlacement[]) => {
    set({ layout: { catalogVersion: 2, version: get().layout.version + 1, widgets }, saveError: null })
    if (!get().isEditMode) queueSave()
  }
  return {
    layout: defaultDashboardLayout(), isEditMode: false, isLoaded: false, isSaving: false, saveError: null, loadError: null,
    loadLayout: async () => {
      const generation = getSessionGeneration()
      const revision = get().layout.version
      try {
        const prefs = await api.preferences.get()
        if (!isCurrentSession(generation) || get().isEditMode || get().layout.version !== revision) return
        set({ layout: prefs.dashboardLayout ? normalizeDashboardLayout(prefs.dashboardLayout) : defaultDashboardLayout(), isLoaded: true, loadError: null })
        if (prefs.dashboardLayoutNeedsSave) queueSave()
      } catch {
        if (isCurrentSession(generation)) set({ isLoaded: true, loadError: "Не удалось загрузить раскладку. Показана локальная версия." })
      }
    },
    saveLayout: async () => {
      if (saveTimer) clearTimeout(saveTimer)
      saveTimer = null
      const generation = getSessionGeneration()
      const layout = get().layout
      set({ isSaving: true, saveError: null })
      let success = false
      // Serialize writes so an earlier response cannot overwrite a newer layout.
      saveChain = saveChain.catch(() => {}).then(async () => {
        if (!isCurrentSession(generation)) return
        try {
          await api.preferences.saveDashboardLayout(layout)
          success = true
        } catch {
          if (isCurrentSession(generation)) set({ saveError: "Раскладка не сохранена на сервере. Повторите сохранение." })
        }
      })
      const pending = saveChain
      await pending
      if (isCurrentSession(generation) && saveChain === pending) set({ isSaving: false, ...(success ? { saveError: null } : {}) })
      return success && isCurrentSession(generation) && get().layout.version === layout.version
    },
    setEditMode: mode => {
      if (saveTimer) clearTimeout(saveTimer)
      saveTimer = null
      set({ isEditMode: mode })
    },
    addWidget: widget => { if (get().layout.widgets.length < 20) change([...get().layout.widgets, widget]) },
    removeWidget: id => change(get().layout.widgets.filter(w => w.instanceId !== id)),
    updatePositions: widgets => { if (JSON.stringify(widgets) !== JSON.stringify(get().layout.widgets)) change(widgets) },
    updateWidgetConfig: (id, config) => change(get().layout.widgets.map(w => w.instanceId === id ? { ...w, config } : w)),
    resetToDefault: () => change(defaultDashboardLayout().widgets),
  }
}, {
  name: "metapus-dashboard", version: 2, partialize: s => ({ layout: s.layout }),
  migrate: persisted => ({ layout: persistedDashboardLayout(persisted) }),
  merge: (persisted, current) => ({ ...current, layout: persistedDashboardLayout(persisted) }),
}))
registerScopedStore(useDashboardStore, () => {
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = null
  useDashboardStore.setState(useDashboardStore.getInitialState(), true)
})

"use client"

import type { JsonObject } from "@/lib/json-value"
import { registerScopedStore, getSessionGeneration, isCurrentSession } from "@/lib/auth-scope"
import { create } from "zustand"
import { persist } from "zustand/middleware"
import { api } from "@/lib/api"
import { MAX_WIDGETS, defaultDashboardLayout, fitRows, layoutWidgets, placeWidget, readBrowserDashboardLayout, removeWidget, updateWidget } from "@/lib/dashboard-layout"
import type { DashboardLayout, WidgetPlacement, WidgetSize } from "@/types/dashboard"

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
  /** Layout as it was on entering edit mode (or after the last save inside it); cancelEdit returns to it. */
  editSnapshot: DashboardLayout | null
  isLoaded: boolean
  isSaving: boolean
  saveError: string | null
  loadError: string | null
  loadLayout: () => Promise<void>
  saveLayout: () => Promise<boolean>
  setEditMode: (mode: boolean) => void
  cancelEdit: () => void
  addWidget: (widget: WidgetPlacement) => void
  removeWidget: (id: string) => void
  /** Rows after a widget was dragged within or between rows. */
  setRows: (rows: WidgetPlacement[][]) => void
  updateWidget: (id: string, patch: { size?: WidgetSize; config?: JsonObject }) => void
  updateWidgetConfig: (id: string, config: JsonObject) => void
  resetToDefault: () => void
}
export const useDashboardStore = create<DashboardState>()(persist((set, get) => {
  const change = (rows: WidgetPlacement[][]) => {
    set({ layout: { catalogVersion: 3, version: get().layout.version + 1, rows }, saveError: null })
    if (!get().isEditMode) queueSave()
  }
  return {
    layout: defaultDashboardLayout(), isEditMode: false, editSnapshot: null, isLoaded: false, isSaving: false, saveError: null, loadError: null,
    loadLayout: async () => {
      const generation = getSessionGeneration()
      const revision = get().layout.version
      try {
        const prefs = await api.preferences.get()
        if (!isCurrentSession(generation) || get().isEditMode || get().layout.version !== revision) return
        set({ layout: prefs.dashboardLayout ?? defaultDashboardLayout(), isLoaded: true, loadError: null })
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
      // The server now holds this layout, so cancelling later edits must not go further back.
      if (success && isCurrentSession(generation) && get().isEditMode) set({ editSnapshot: layout })
      return success && isCurrentSession(generation) && get().layout.version === layout.version
    },
    setEditMode: mode => {
      // A change made just before entering edit mode is still debounced: write it now, before edits begin.
      if (mode && saveTimer) void get().saveLayout()
      if (saveTimer) clearTimeout(saveTimer)
      saveTimer = null
      set({ isEditMode: mode, editSnapshot: mode ? get().layout : null })
    },
    cancelEdit: () => {
      const { editSnapshot, layout, isEditMode } = get()
      if (!isEditMode) return
      // A new version number keeps in-flight saves and loads from treating the restored layout as theirs.
      set({ isEditMode: false, editSnapshot: null, saveError: null, ...(editSnapshot ? { layout: { ...editSnapshot, version: layout.version + 1 } } : {}) })
    },
    addWidget: widget => { if (layoutWidgets(get().layout).length < MAX_WIDGETS) change(placeWidget(get().layout.rows, widget)) },
    removeWidget: id => change(removeWidget(get().layout.rows, id)),
    setRows: rows => {
      const next = fitRows(rows)
      if (JSON.stringify(next) !== JSON.stringify(get().layout.rows)) change(next)
    },
    updateWidget: (id, patch) => change(updateWidget(get().layout.rows, id, patch)),
    updateWidgetConfig: (id, config) => change(updateWidget(get().layout.rows, id, { config })),
    resetToDefault: () => change(defaultDashboardLayout().rows),
  }
}, {
  name: "metapus-dashboard", version: 3, partialize: s => ({ layout: s.layout }),
  merge: (persisted, current) => ({ ...current, layout: readBrowserDashboardLayout(persisted) }),
}))
registerScopedStore(useDashboardStore, () => {
  if (saveTimer) clearTimeout(saveTimer)
  saveTimer = null
  useDashboardStore.setState(useDashboardStore.getInitialState(), true)
})

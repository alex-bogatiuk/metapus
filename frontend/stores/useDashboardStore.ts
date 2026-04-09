"use client"

import { create } from "zustand"
import { persist } from "zustand/middleware"
import { api } from "@/lib/api"
import type { DashboardLayout, WidgetPlacement } from "@/types/dashboard"

// ── Default layout (mirrors current hardcoded page.tsx) ─────────────

const DEFAULT_LAYOUT: DashboardLayout = {
    version: 1,
    widgets: [
        { instanceId: "default-kpi-cash", widgetType: "kpi", x: 0, y: 0, w: 3, h: 1, config: { metric: "cash-balance" } },
        { instanceId: "default-kpi-stock", widgetType: "kpi", x: 3, y: 0, w: 3, h: 1, config: { metric: "stock-value" } },
        { instanceId: "default-kpi-recv", widgetType: "kpi", x: 6, y: 0, w: 3, h: 1, config: { metric: "receivables" } },
        { instanceId: "default-kpi-payables", widgetType: "kpi", x: 9, y: 0, w: 3, h: 1, config: { metric: "payables" } },
        { instanceId: "default-recent-docs", widgetType: "recent-documents", x: 0, y: 1, w: 8, h: 3, config: { limit: 5, documentTypes: [] } },
        { instanceId: "default-quick-act", widgetType: "quick-actions", x: 8, y: 1, w: 4, h: 2, config: {} },
        { instanceId: "default-tasks", widgetType: "tasks", x: 8, y: 3, w: 4, h: 2, config: {} },
    ],
}

// ── Debounce helper ─────────────────────────────────────────────────

let saveTimer: ReturnType<typeof setTimeout> | null = null

function debouncedSaveLayout(layout: DashboardLayout) {
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
        api.preferences.saveDashboardLayout(layout).catch((err) => {
            console.error("Failed to save dashboard layout:", err)
        })
    }, 800)
}

// ── Store ───────────────────────────────────────────────────────────

interface DashboardState {
    layout: DashboardLayout
    isEditMode: boolean
    isLoaded: boolean

    loadLayout: () => Promise<void>
    setEditMode: (mode: boolean) => void
    addWidget: (widget: WidgetPlacement) => void
    removeWidget: (instanceId: string) => void
    updatePositions: (widgets: WidgetPlacement[]) => void
    updateWidgetConfig: (instanceId: string, config: Record<string, unknown>) => void
    resetToDefault: () => void
}

export const useDashboardStore = create<DashboardState>()(
    persist(
        (set, get) => ({
            layout: DEFAULT_LAYOUT,
            isEditMode: false,
            isLoaded: false,

            loadLayout: async () => {
                try {
                    const prefs = await api.preferences.get()
                    if (prefs.dashboardLayout && prefs.dashboardLayout.widgets) {
                        set({ layout: prefs.dashboardLayout, isLoaded: true })
                    } else {
                        set({ isLoaded: true })
                    }
                } catch (err) {
                    console.error("Failed to load dashboard layout:", err)
                    set({ isLoaded: true })
                }
            },

            setEditMode: (mode) => {
                set({ isEditMode: mode })
                if (!mode) {
                    debouncedSaveLayout(get().layout)
                }
            },

            addWidget: (widget) => {
                const { layout } = get()
                if (layout.widgets.length >= 20) return
                const updated: DashboardLayout = {
                    version: layout.version + 1,
                    widgets: [...layout.widgets, widget],
                }
                set({ layout: updated })
                debouncedSaveLayout(updated)
            },

            removeWidget: (instanceId) => {
                const { layout } = get()
                const updated: DashboardLayout = {
                    version: layout.version + 1,
                    widgets: layout.widgets.filter((w) => w.instanceId !== instanceId),
                }
                set({ layout: updated })
                debouncedSaveLayout(updated)
            },

            updatePositions: (widgets) => {
                const { layout } = get()
                const updated: DashboardLayout = {
                    version: layout.version + 1,
                    widgets,
                }
                set({ layout: updated })
                debouncedSaveLayout(updated)
            },

            updateWidgetConfig: (instanceId, config) => {
                const { layout } = get()
                const updated: DashboardLayout = {
                    version: layout.version + 1,
                    widgets: layout.widgets.map((w) =>
                        w.instanceId === instanceId ? { ...w, config } : w
                    ),
                }
                set({ layout: updated })
                debouncedSaveLayout(updated)
            },

            resetToDefault: () => {
                const updated = { ...DEFAULT_LAYOUT, version: get().layout.version + 1 }
                set({ layout: updated })
                debouncedSaveLayout(updated)
            },
        }),
        {
            name: "metapus-dashboard",
            partialize: (s) => ({ layout: s.layout }),
        }
    )
)

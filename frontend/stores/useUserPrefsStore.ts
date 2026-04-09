/**
 * User preferences store — per-user UI settings persisted to the backend.
 *
 * - `interface` prefs (theme, pageSize, sidebar) are persisted in localStorage
 *   for instant rendering (avoid FOUC), then synced to the server with debounce.
 * - `listFilters` and `listColumns` are stored server-side only (fire-and-forget).
 * - `listColumnWidths` are persisted via `list-columns/:entityType` with a `__widths` namespace key.
 */

import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { InterfacePrefs } from "@/types/user-prefs"
import type { FilterValues } from "@/lib/filter-utils"
import { api } from "@/lib/api"

// ── Debounce helpers ────────────────────────────────────────────────────

let interfaceTimer: ReturnType<typeof setTimeout> | null = null

function debouncedSaveInterface(patch: Partial<InterfacePrefs>) {
    if (interfaceTimer) clearTimeout(interfaceTimer)
    interfaceTimer = setTimeout(() => {
        api.preferences.saveInterface(patch).catch((err) => {
            console.error("Failed to save interface preferences:", err)
        })
    }, 500)
}

const widthTimers = new Map<string, ReturnType<typeof setTimeout>>()

function debouncedSaveColumnWidths(entityType: string, widths: Record<string, number>) {
    const existing = widthTimers.get(entityType)
    if (existing) clearTimeout(existing)
    widthTimers.set(entityType, setTimeout(() => {
        widthTimers.delete(entityType)
        // Store widths under a namespaced key "__widths" to avoid collision
        // with visible column arrays that are stored as plain string[].
        api.preferences.saveListColumns(
            `${entityType}__widths`,
            widths as unknown as string[],
        ).catch((err) => {
            console.error(`Failed to save column widths for ${entityType}:`, err)
        })
    }, 5000))
}

// ── Store ───────────────────────────────────────────────────────────────

interface UserPrefsState {
    // Data
    interface: InterfacePrefs
    listFilters: Record<string, FilterValues>
    listColumns: Record<string, string[]>
    listColumnWidths: Record<string, Record<string, number>>
    isLoaded: boolean

    // Actions
    loadPreferences: () => Promise<void>
    updateInterface: (patch: Partial<InterfacePrefs>) => void
    setListFilters: (entityType: string, values: FilterValues) => void
    setListColumns: (entityType: string, columns: string[]) => void
    setListColumnWidths: (entityType: string, widths: Record<string, number>) => void
    getListFilters: (entityType: string) => FilterValues
    getListColumns: (entityType: string) => string[]
    getListColumnWidths: (entityType: string) => Record<string, number> | undefined
    reset: () => void
}

const initialState = {
    interface: {} as InterfacePrefs,
    listFilters: {} as Record<string, FilterValues>,
    listColumns: {} as Record<string, string[]>,
    listColumnWidths: {} as Record<string, Record<string, number>>,
    isLoaded: false,
}

export const useUserPrefsStore = create<UserPrefsState>()(
    persist(
        (set, get) => ({
            ...initialState,

            loadPreferences: async () => {
                try {
                    const data = await api.preferences.get()
                    // Separate column widths from visible columns.
                    // Widths are stored with "__widths" suffix keys.
                    const listColumns: Record<string, string[]> = {}
                    const listColumnWidths: Record<string, Record<string, number>> = {}
                    if (data.listColumns) {
                        for (const [key, value] of Object.entries(data.listColumns)) {
                            if (key.endsWith("__widths")) {
                                const entityKey = key.replace("__widths", "")
                                listColumnWidths[entityKey] = value as unknown as Record<string, number>
                            } else {
                                listColumns[key] = value
                            }
                        }
                    }
                    // Also parse widths from dedicated field if backend returns it.
                    if (data.listColumnWidths) {
                        Object.assign(listColumnWidths, data.listColumnWidths)
                    }
                    set({
                        interface: data.interface ?? {},
                        listFilters: data.listFilters ?? {},
                        listColumns,
                        listColumnWidths,
                        isLoaded: true,
                    })
                } catch (err) {
                    console.error("Failed to load preferences:", err)
                    // Mark as loaded even on failure — use localStorage fallback
                    set({ isLoaded: true })
                }
            },

            updateInterface: (patch) => {
                set((s) => ({
                    interface: { ...s.interface, ...patch },
                }))
                debouncedSaveInterface({ ...get().interface, ...patch })

                // Immediate DOM side-effects for theme and accent color
                if (patch.theme !== undefined) {
                    const root = document.documentElement
                    if (patch.theme === "dark") {
                        root.classList.add("dark")
                    } else if (patch.theme === "system") {
                        const isDark = window.matchMedia("(prefers-color-scheme: dark)").matches
                        root.classList.toggle("dark", isDark)
                    } else {
                        root.classList.remove("dark")
                    }
                }
                if (patch.accentColor !== undefined) {
                    const root = document.documentElement
                    if (patch.accentColor === "yellow" || !patch.accentColor) {
                        root.removeAttribute("data-accent")
                    } else {
                        root.setAttribute("data-accent", patch.accentColor)
                    }
                }
            },

            setListFilters: (entityType, values) => {
                set((s) => ({
                    listFilters: { ...s.listFilters, [entityType]: values },
                }))
                // Fire-and-forget
                api.preferences.saveListFilters(entityType, values).catch((err) => {
                    console.error(`Failed to save list filters for ${entityType}:`, err)
                })
            },

            setListColumns: (entityType, columns) => {
                set((s) => ({
                    listColumns: { ...s.listColumns, [entityType]: columns },
                }))
                api.preferences.saveListColumns(entityType, columns).catch((err) => {
                    console.error(`Failed to save list columns for ${entityType}:`, err)
                })
            },

            setListColumnWidths: (entityType, widths) => {
                set((s) => ({
                    listColumnWidths: { ...s.listColumnWidths, [entityType]: widths },
                }))
                debouncedSaveColumnWidths(entityType, widths)
            },

            getListFilters: (entityType) => get().listFilters[entityType] ?? {},

            getListColumns: (entityType) => get().listColumns[entityType] ?? [],

            getListColumnWidths: (entityType) => get().listColumnWidths[entityType],

            reset: () => set(initialState),
        }),
        {
            name: "metapus-user-prefs",
            // Persist only interface prefs (theme) for instant FOUC-free rendering.
            // Filters and columns are loaded from server on each session.
            partialize: (s) => ({ interface: s.interface }),
        }
    )
)

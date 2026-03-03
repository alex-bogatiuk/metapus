/**
 * User preferences store — per-user UI settings persisted to the backend.
 *
 * - `interface` prefs (theme, pageSize, sidebar) are persisted in localStorage
 *   for instant rendering (avoid FOUC), then synced to the server with debounce.
 * - `listFilters` and `listColumns` are stored server-side only (fire-and-forget).
 */

import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { InterfacePrefs } from "@/types/user-prefs"
import type { FilterValues } from "@/lib/filter-utils"
import { api } from "@/lib/api"

// ── Debounce helper ─────────────────────────────────────────────────────

let interfaceTimer: ReturnType<typeof setTimeout> | null = null

function debouncedSaveInterface(patch: Partial<InterfacePrefs>) {
    if (interfaceTimer) clearTimeout(interfaceTimer)
    interfaceTimer = setTimeout(() => {
        api.preferences.saveInterface(patch).catch((err) => {
            console.error("Failed to save interface preferences:", err)
        })
    }, 500)
}

// ── Store ───────────────────────────────────────────────────────────────

interface UserPrefsState {
    // Data
    interface: InterfacePrefs
    listFilters: Record<string, FilterValues>
    listColumns: Record<string, string[]>
    isLoaded: boolean

    // Actions
    loadPreferences: () => Promise<void>
    updateInterface: (patch: Partial<InterfacePrefs>) => void
    setListFilters: (entityType: string, values: FilterValues) => void
    setListColumns: (entityType: string, columns: string[]) => void
    getListFilters: (entityType: string) => FilterValues
    getListColumns: (entityType: string) => string[]
    reset: () => void
}

const initialState = {
    interface: {} as InterfacePrefs,
    listFilters: {} as Record<string, FilterValues>,
    listColumns: {} as Record<string, string[]>,
    isLoaded: false,
}

export const useUserPrefsStore = create<UserPrefsState>()(
    persist(
        (set, get) => ({
            ...initialState,

            loadPreferences: async () => {
                try {
                    const data = await api.preferences.get()
                    set({
                        interface: data.interface ?? {},
                        listFilters: data.listFilters ?? {},
                        listColumns: data.listColumns ?? {},
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

            getListFilters: (entityType) => get().listFilters[entityType] ?? {},

            getListColumns: (entityType) => get().listColumns[entityType] ?? [],

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

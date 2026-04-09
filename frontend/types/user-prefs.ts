/**
 * User preferences types — mirrors backend userpref domain.
 *
 * InterfacePrefs  → typed UI settings (partial updates supported)
 * listFilters       → opaque FilterValues per entity type
 * listColumns       → column key arrays per entity type
 * listColumnWidths  → column pixel widths per entity type (persisted server-side in list_columns JSONB)
 */

import type { FilterValues } from "@/lib/filter-utils"

// ── Interface preferences ───────────────────────────────────────────────

export type ThemeMode = "light" | "dark" | "system"
export type AccentColor = "yellow" | "neutral" | "blue"
export type DateFormat = "dd.MM.yyyy" | "yyyy-MM-dd" | "MM/dd/yyyy"
export type NumberFormat = "space" | "comma" | "none"

export interface InterfacePrefs {
    theme?: ThemeMode
    accentColor?: AccentColor
    language?: string
    dateFormat?: DateFormat
    numberFormat?: NumberFormat
    pageSize?: number
    showTooltips?: boolean
    compactMode?: boolean
    sidebarCollapsed?: boolean
    /** Per-entity toggle: show deletion-marked items in list views. Key = entity type (e.g. "GoodsReceipt"). */
    showDeletedEntities?: Record<string, boolean>
}

// ── API response ────────────────────────────────────────────────────────

export interface UserPreferencesResponse {
    userId: string
    interface: InterfacePrefs
    listFilters: Record<string, FilterValues>
    listColumns: Record<string, string[]>
    listColumnWidths: Record<string, Record<string, number>>
    dashboardLayout: import("@/types/dashboard").DashboardLayout | null
    updatedAt: string
}

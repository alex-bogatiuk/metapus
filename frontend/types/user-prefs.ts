/**
 * User preferences types — mirrors backend userpref domain.
 *
 * InterfacePrefs  → typed UI settings (partial updates supported)
 * listFilters     → opaque FilterValues per entity type
 * listColumns     → column key arrays per entity type
 */

import type { FilterValues } from "@/lib/filter-utils"

// ── Interface preferences ───────────────────────────────────────────────

export type ThemeMode = "light" | "dark" | "system"
export type DateFormat = "dd.MM.yyyy" | "yyyy-MM-dd" | "MM/dd/yyyy"
export type NumberFormat = "space" | "comma" | "none"

export interface InterfacePrefs {
    theme?: ThemeMode
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
    updatedAt: string
}

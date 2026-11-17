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
export type DateFormat = "dd.MM.yyyy" | "yyyy-MM-dd" | "MM/dd/yyyy"
export type NumberFormat = "space" | "comma" | "none"

export interface InterfacePrefs {
  theme?: ThemeMode
  language?: string
  dateFormat?: DateFormat
  numberFormat?: NumberFormat
  showTooltips?: boolean
  compactMode?: boolean
  sidebarCollapsed?: boolean
  /** Table parts work like 1C: a click selects a cell, a double click, Enter or F2 edits it. */
  tablePartCellMode?: boolean
  /** Per-entity toggle: show deletion-marked items in list views. Key = entity type (e.g. "Counterparty"). */
  showDeletedEntities?: Record<string, boolean>
  /** Per-section toggle: whether a collapsible UI section is expanded. Key = section id owned by the UI. */
  expandedSections?: Record<string, boolean>
}

// ── Favorites ───────────────────────────────────────────────────────────

/** A bookmarked entity. Title is cached at add-time and self-heals on next open. */
export interface FavoriteItem {
  entityType: string   // registry key: "counterparty" | "goods_receipt" | …
  entityId: string     // UUID
  title: string        // cached presentation (self-heals on form open)
  url: string          // direct link (e.g. "/catalogs/counterparties/uuid")
  addedAt: string      // ISO 8601
}

// ── API response ────────────────────────────────────────────────────────

export interface UserPreferencesResponse {
  userId: string
  interface: InterfacePrefs
  listFilters: Record<string, FilterValues>
  listColumns: Record<string, string[] | Record<string, number>>
  dashboardLayout: import("@/types/dashboard").DashboardLayout | null
  favorites: FavoriteItem[]
  updatedAt: string
}

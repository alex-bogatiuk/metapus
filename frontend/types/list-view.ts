/**
 * List view types — mirrors backend listview domain.
 *
 * A list view is a named combination of filters, visible columns, and sort order
 * that users can save and restore for any entity list page.
 */

import type { FilterValues } from "@/lib/filter-utils"

// ── Visibility ──────────────────────────────────────────────────────────

export type ViewVisibility = "personal" | "shared" | "system"

// ── Config ──────────────────────────────────────────────────────────────

export interface ListViewConfig {
    filters: FilterValues
    columns: string[]
    sortColumn: string | null
    sortDir: "asc" | "desc"
}

// ── Response ────────────────────────────────────────────────────────────

export interface ListView {
    id: string
    entityType: string
    name: string
    authorId: string | null
    visibility: ViewVisibility
    isDefault: boolean
    sortOrder: number
    config: ListViewConfig
    version: number
    createdAt: string
    updatedAt: string
}

// ── Requests ────────────────────────────────────────────────────────────

export interface CreateListViewRequest {
    entityType: string
    name: string
    visibility: ViewVisibility
    isDefault: boolean
    config: ListViewConfig
}

export interface UpdateListViewRequest {
    name: string
    visibility: ViewVisibility
    isDefault: boolean
    config: ListViewConfig
    version: number
}

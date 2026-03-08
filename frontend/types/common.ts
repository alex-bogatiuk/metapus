/**
 * Common / cross-cutting types used across the frontend.
 * Mirrors: internal/infrastructure/http/v1/dto/common.go
 */

/** Sort direction for list views. */
export type SortDirection = "asc" | "desc"

/** Generic sort parameters stored in URL search params. */
export interface SortParams {
    column: string | null
    direction: SortDirection
}

// ── Pagination ──────────────────────────────────────────────────────────

/** Comparison operator for advanced filters (mirrors backend filter.ComparisonType). */
export type ComparisonOperator =
    | "eq" | "neq" | "lt" | "lte" | "gt" | "gte"
    | "in" | "nin" | "contains" | "ncontains"
    | "null" | "not_null"
    | "in_hierarchy" | "nin_hierarchy"

/** Single advanced filter item sent to the backend as part of ?filter= JSON. */
export interface AdvancedFilterItem {
    field: string
    fieldType?: string
    operator: ComparisonOperator
    value?: unknown
    /** Storage multiplier for scaled numeric types (e.g. 10000 for Quantity, 100 for Money). */
    scale?: number
}

/** Cursor-paginated list response envelope from the API. */
export interface CursorListResponse<T> {
    items: T[]
    nextCursor?: string
    prevCursor?: string
    hasMore: boolean
    hasPrev: boolean
    targetIndex?: number
    totalCount: number
}

/** @deprecated Use CursorListResponse<T> */
export type ListResponse<T> = CursorListResponse<T>

/** Query parameters for cursor-paginated list endpoints. */
export interface CursorListParams {
    limit?: number
    search?: string
    orderBy?: string
    /** Include soft-deleted (deletion-marked) items */
    includeDeleted?: boolean
    /** Advanced filters — serialized as JSON in ?filter= query param */
    filter?: AdvancedFilterItem[]
    /** Opaque cursor — load items AFTER this cursor (scroll down) */
    after?: string
    /** Opaque cursor — load items BEFORE this cursor (scroll up) */
    before?: string
    /** Target ID — teleport to this item in the list */
    around?: string
}

/** @deprecated Use CursorListParams */
export type ListParams = CursorListParams

// ── Base entity fields ──────────────────────────────────────────────────

/** Fields common to all catalog/document responses (mirrors BaseResponse). */
export interface BaseEntity {
    id: string
    createdAt: string
    updatedAt: string
}

/** Fields common to all catalog entity responses (mirrors CatalogResponse). */
export interface BaseCatalogEntity extends BaseEntity {
    code: string
    name: string
    isFolder: boolean
    parentId: string | null
    deletionMark: boolean
}

/** Fields common to all document entity responses (mirrors DocumentResponse). */
export interface BaseDocumentEntity extends BaseEntity {
    number: string
    date: string
    posted: boolean
    deletionMark: boolean
}

/** @deprecated Use ListResponse<T> instead */
export interface PaginatedResponse<T> {
    items: T[]
    total: number
    page: number
    pageSize: number
}

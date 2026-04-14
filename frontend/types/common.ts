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
    | "between"

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

// ── Polymorphic References ──────────────────────────────────────────────

/**
 * Universal polymorphic reference (mirrors entity.TypedRef).
 * Like 1C's "ОписаниеТипов" / "СоставнойТипДанных" or ERPNext's "Dynamic Link".
 *
 * Works with both documents and catalogs:
 * - Document ref: { refType: "GoodsReceipt", refId: "uuid" }
 * - Catalog ref:  { refType: "Counterparty", refId: "uuid" }
 *
 * Used in table parts where a line may reference different entity types
 * (e.g. a bank statement line referencing CashReceipt or CashPayment).
 */
export interface TypedRef {
    /** Entity type name from metadata registry (e.g. "GoodsReceipt", "Counterparty") */
    refType: string
    /** UUID of the referenced entity */
    refId: string
}

/**
 * Resolved presentation of a TypedRef.
 * Returned by POST /api/v1/resolve-refs endpoint.
 */
export interface ResolvedRef {
    refType: string
    refId: string
    /** Human-readable presentation, e.g. "Поступление товаров ПТ-00042 от 15.03.2026" */
    presentation: string
    /** Entity category: "catalog" or "document" */
    entityType: "catalog" | "document"
}

/**
 * Found reference — a single incoming reference to a target entity.
 * Returned by POST /api/v1/system/find-references.
 */
export interface FoundReference {
    sourceEntityName: string
    sourceEntityType: "catalog" | "document"
    sourceField: string
    sourceId: string
    presentation: string
}

/**
 * Marked object — an entity with deletion mark, enriched with reference count.
 * Returned by GET /api/v1/system/marked-objects.
 */
export interface MarkedObject {
    entityName: string
    entityType: "catalog" | "document"
    entityId: string
    presentation: string
    refCount: number
    canDelete: boolean
}



// ── Related Documents ───────────────────────────────────────────────────

/**
 * A group of related documents of the same type (flat FK-references).
 * Returned by GET /api/v1/document/{type}/{id}/related-documents.
 */
export interface RelatedDocGroup {
    entityName: string           // e.g. "GoodsIssue"
    entityType: "catalog" | "document"
    presentation: string         // e.g. "Реализации товаров"
    routePrefix: string          // e.g. "goods-issue"
    items: RelatedDocItem[]
    totalCount: number
}

/** A single related document in a group or tree node. */
export interface RelatedDocItem {
    id: string
    presentation: string         // e.g. "РТ-00015  15.03.2026"
    number: string
    date: string
    posted: boolean
    deletionMark: boolean
    amount?: number
    currencyId?: string
    /** Resolved preview fields: label → value (e.g. "Поставщик" → "ООО Ромашка") */
    previewData?: Record<string, string>
}

/** A node in the document subordination tree. */
export interface RelatedDocTreeNode extends RelatedDocItem {
    entityName: string
    entityType: "document" | "catalog"
    routePrefix: string
    isCurrent: boolean
    children?: RelatedDocTreeNode[]
}

/** Response from GET /document/{type}/{id}/related-documents. */
export interface RelatedDocumentsResponse {
    /** Subordination tree (root = top-level document in the chain) */
    tree?: RelatedDocTreeNode
    /** FK-referenced documents NOT in the basis chain */
    flatGroups?: RelatedDocGroup[]
    /** Total documents across tree + flat groups */
    total: number
}


// ── Generic Movement Types ──────────────────────────────────────────────

/**
 * A generic representation of a movement in a register (accumulation or info).
 * Equivalent to entity.DocumentMovement on the backend.
 */
export interface DocumentMovement {
    registerName: string
    recordType: "receipt" | "expense"
    period: string
    columns: MovementColumnDef[]
    data: Record<string, string | number | boolean | null | MovementRefValue>
}

export interface MovementColumnDef {
    key: string
    label: string
    type: "ref" | "amount" | "quantity" | "text"
}

export interface MovementRefValue {
    id: string
    name: string
    url?: string
}

/**
 * Response payload for fetching movements of a document.
 */
export interface DocumentMovementsResponse {
    movements: DocumentMovement[]
    count: number
}

// ── Batch Operations ────────────────────────────────────────────────────

/** Action types for batch document operations. */
export type BatchActionType = "post" | "unpost" | "setDeletionMark" | "clearDeletionMark"

/** Per-item result from a batch operation. */
export interface BatchActionResult {
    id: string
    success: boolean
    error?: string
}

/** Response from POST /document/{type}/batch-action. */
export interface BatchActionResponse {
    results: BatchActionResult[]
    total: number
    success: number
    failed: number
}

/** Request for POST /document/{type}/batch-action-by-filter. */
export interface BatchActionByFilterRequest {
    /** JSON-encoded filter items matching the current list view. */
    filter: AdvancedFilterItem[]
    /** Action to perform on all matching documents. */
    action: BatchActionType
    /** IDs to exclude (user manually unchecked during virtual select all). */
    excludeIds?: string[]
    /** Whether to include deleted items (match current list view). */
    includeDeleted?: boolean
    /** Current sort order (for filter consistency). */
    orderBy?: string
    /** Current search text. */
    search?: string
}

/** SSE event emitted during streaming batch operations. */
export interface BatchProgressEvent {
    type: "started" | "progress" | "completed" | "cancelled"
    processed: number
    success: number
    failed: number
    total: number
}

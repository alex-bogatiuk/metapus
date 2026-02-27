/**
 * Shared types for Document entities.
 * Mirrors: internal/infrastructure/http/v1/dto/goods_receipt.go
 */

// ── Common ──────────────────────────────────────────────────────────────

/** Lightweight display representation of a referenced catalog entity.
 *  Mirrors: internal/infrastructure/storage/postgres.RefDisplay */
export interface RefDisplay {
    id: string
    name: string
}

/** Possible document statuses (derived from `posted` boolean). */
export type DocumentStatus = "draft" | "posted"

/** Request DTO for setting/clearing deletion mark on a document. */
export interface SetDocumentDeletionMarkRequest {
    deletionMark: boolean
}

// ── Goods Receipt — Response ────────────────────────────────────────────

/** Response DTO for a goods receipt line. Mirrors GoodsReceiptLineResponse. */
export interface GoodsReceiptLineResponse {
    lineId: string
    lineNo: number
    productId: string
    unitId: string
    coefficient: string   // decimal
    quantity: number       // int64 (Quantity scaled ×10000)
    unitPrice: number      // int64 (MinorUnits — kopecks)
    discountPercent: string // decimal
    discountAmount: number  // int64
    vatRateId: string
    vatAmount: number       // int64
    amount: number          // int64
    // Resolved reference display names
    product?: RefDisplay
    unit?: RefDisplay
    vatRate?: RefDisplay
}

/** Response DTO for a goods receipt document. Mirrors GoodsReceiptResponse. */
export interface GoodsReceiptResponse {
    id: string
    number: string
    date: string           // ISO datetime
    posted: boolean
    postedVersion?: number
    organizationId: string
    supplierId: string
    contractId?: string | null
    warehouseId: string
    supplierDocNumber?: string
    supplierDocDate?: string | null  // ISO datetime
    incomingNumber?: string | null
    currencyId: string
    amountIncludesVat: boolean
    totalQuantity: number   // int64 (Quantity)
    totalAmount: number     // int64 (MinorUnits)
    totalVat: number        // int64 (MinorUnits)
    description?: string
    lines: GoodsReceiptLineResponse[]
    deletionMark: boolean
    createdAt: string       // ISO datetime
    updatedAt: string       // ISO datetime
    // Resolved reference display names
    organization?: RefDisplay
    supplier?: RefDisplay
    contract?: RefDisplay
    warehouse?: RefDisplay
    currency?: RefDisplay
}

// ── Goods Receipt — Requests ────────────────────────────────────────────

/** Request DTO for a goods receipt line (create/update). */
export interface GoodsReceiptLineRequest {
    productId: string
    unitId: string
    coefficient?: string    // decimal, defaults to "1"
    quantity: number        // int64 (Quantity)
    unitPrice: number       // int64 (MinorUnits)
    vatRateId: string
    vatPercent?: number
    discountPercent?: string // decimal
}

/** Request DTO for creating a goods receipt. Mirrors CreateGoodsReceiptRequest. */
export interface CreateGoodsReceiptRequest {
    number?: string
    date: string            // ISO datetime
    organizationId: string
    supplierId: string
    contractId?: string | null
    warehouseId: string
    supplierDocNumber?: string
    supplierDocDate?: string | null
    incomingNumber?: string | null
    currencyId?: string
    amountIncludesVat?: boolean
    description?: string
    lines: GoodsReceiptLineRequest[]
    postImmediately?: boolean
}

/** Request DTO for updating a goods receipt. Mirrors UpdateGoodsReceiptRequest. */
export interface UpdateGoodsReceiptRequest {
    number?: string | null
    date?: string | null
    organizationId?: string | null
    supplierId?: string | null
    contractId?: string | null
    warehouseId?: string | null
    supplierDocNumber?: string | null
    supplierDocDate?: string | null
    incomingNumber?: string | null
    currencyId?: string | null
    amountIncludesVat?: boolean | null
    description?: string | null
    lines?: GoodsReceiptLineRequest[]
}

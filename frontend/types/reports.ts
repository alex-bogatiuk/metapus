/**
 * Report types — mirrors backend dto/reports.go
 */

// ── Stock Balance Report ────────────────────────────────────────────

export interface StockBalanceReportResponse {
    asOfDate: string
    items: StockBalanceReportItem[]
    totalItems: number
    totalQuantity: number
}

export interface StockBalanceReportItem {
    warehouseId: string
    warehouseName: string
    nomenclatureId: string
    nomenclatureName: string
    nomenclatureSku?: string
    unitName?: string
    quantity: number
}

// ── Stock Turnover Report ───────────────────────────────────────────

export interface StockTurnoverReportResponse {
    fromDate: string
    toDate: string
    items: StockTurnoverReportItem[]
    totalItems: number
    totalOpening: number
    totalReceipt: number
    totalExpense: number
    totalClosing: number
}

export interface StockTurnoverReportItem {
    warehouseId?: string
    warehouseName?: string
    nomenclatureId?: string
    nomenclatureName?: string
    nomenclatureSku?: string
    unitName?: string
    openingBalance: number
    receipt: number
    expense: number
    closingBalance: number
}

// ── Document Journal ────────────────────────────────────────────────

export interface DocumentJournalResponse {
    items: DocumentJournalItem[]
    totalCount: number
    limit: number
    offset: number
    summary?: DocumentTypeSummary[]
}

export interface DocumentJournalItem {
    id: string
    documentType: string
    number: string
    date: string
    posted: boolean
    counterpartyId?: string | null
    counterpartyName?: string
    warehouseId?: string | null
    warehouseName?: string
    totalQuantity: number
    totalAmount: number
    currency: string
    description?: string
    deletionMark?: boolean
    createdAt: string
    updatedAt: string
}

export interface DocumentTypeSummary {
    documentType: string
    count: number
    postedCount: number
    totalQuantity: number
    totalAmount: number
}

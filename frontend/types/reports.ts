import type { CryptoUnits } from "@/lib/money"

/**
 * Report types — mirrors backend report datasets and journal DTOs.
 */

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
  totalAmount: CryptoUnits | null
  currency: string
  currencyId: string | null
  decimalPlaces: number | null
  description?: string
  deletionMark?: boolean
  createdAt: string
  updatedAt: string
}

export interface DocumentTypeSummary {
  documentType: string
  count: number
  postedCount: number
  totalAmount: CryptoUnits | null
}

/**
 * Shared types for Document entities.
 */

import { CurrencyRefDisplay, RefDisplay } from "@/types/common"

export type { CurrencyRefDisplay, RefDisplay }

// ── Common ──────────────────────────────────────────────────────────────

/** Possible document statuses (derived from `posted` boolean). */
export type DocumentStatus = "draft" | "posted"

/** Request DTO for setting/clearing deletion mark on a document. Mirrors dto.SetDeletionMarkRequest (json:"marked"). */
export interface SetDocumentDeletionMarkRequest {
  marked: boolean
}

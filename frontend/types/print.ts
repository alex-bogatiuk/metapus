// frontend/types/print.ts
// TS mirror of Go printing.PrintFormSummary DTO.

/** Category of a print form — matches Go PrintFormCategory. */
export type PrintFormCategory = "standard" | "custom"

/** Lightweight descriptor returned by GET /document/{type}/print-forms. */
export interface PrintFormSummary {
  /** Machine-readable form identifier, e.g. "standard". */
  name: string
  /** Human-readable form label, e.g. "Поступление товаров". */
  label: string
  /** Category for UI grouping. */
  category: PrintFormCategory
}

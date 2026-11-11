// frontend/types/print.ts
// Print form descriptors come from the generated HTTP contract of
// GET /document/{type}/print-forms; there is no hand-written mirror.

import type * as Wire from "@/types/generated/http-contracts"

/** Lightweight descriptor returned by GET /document/{type}/print-forms. */
export type PrintFormSummary = Wire.PrintingPrintFormSummary

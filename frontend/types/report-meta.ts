import type { JsonObject, JsonValue } from "@/lib/json-value"

/**
 * Report metadata types — mirrors backend platform.ReportMeta.
 *
 * These types are returned by GET /reports/{key}/metadata
 * and used by useReportPage to auto-generate filter controls,
 * table columns, grouping, and totals.
 */

// ── Report Metadata ─────────────────────────────────────────────────────

export interface ReportMeta {
  key: string
  name: string
  description?: string
  filters: ReportFilterDef[]
  columns: ReportColumnDef[]
  groupBy: ReportGroupByDef[]
  totals: ReportTotalDef[]
  exportFormats: string[]
  scopeDimensions: string[]
  defaultSort?: ReportSortDef
  /** Auto-discovery tree of selectable fields (from Query Engine). */
  availableFields?: FieldTreeNode[]
}

/** Node in the field selection tree for the Report Builder UI. */
export interface FieldTreeNode {
  /** Full dot-separated path, e.g. "wallet_id.merchant_id.name" */
  key: string
  /** Short field name, e.g. "name" */
  name: string
  /** Human-readable label, e.g. "Наименование" */
  label: string
  /** Field data type: "string", "quantity", "ref", etc. */
  type: string
  /** Field role: "dimension", "measure", "attribute" */
  kind: string
  /** Nested fields for ref-type nodes */
  children?: FieldTreeNode[]
  /** Whether the field supports sorting */
  sortable?: boolean
  /** Route prefix for navigation (only for type="ref") */
  refRoute?: string
  /** Allowed options for type="enum" fields (dropdown values) */
  enumValues?: { value: string; label: string }[]
}

export interface ReportFilterDef {
  key: string
  type: "date" | "period" | "reference" | "boolean" | "enum" | "string"
  label: string
  required?: boolean
  /** Entity name for reference picker (e.g. "merchant") */
  ref?: string
  /** Allow multiple values */
  multi?: boolean
  /** Default value */
  default?: JsonValue
}

export interface ReportColumnDef {
  money?: MoneySources
  key: string
  label: string
  type: "string" | "quantity" | "money" | "date" | "reference" | "boolean"
  align?: "left" | "center" | "right"
  sortable?: boolean
  defaultHidden?: boolean
  format?: "number" | "currency" | "percent"
  /** For reference columns: the column key containing the raw UUID (e.g. "merchant_id") */
  refIdKey?: string
  /** For reference columns: entity route prefix for navigation (e.g. "merchants") */
  refRoute?: string
}

export interface ReportGroupByDef {
  key: string
  label: string
  defaultActive?: boolean
}

export interface ReportTotalDef {
  money?: MoneySources
  column: string
  func: "sum" | "count" | "avg" | "min" | "max"
  label?: string
}

export interface ReportSortDef {
  column: string
  direction: "asc" | "desc"
}

// ── Display Rows (frontend grouping) ────────────────────────────────────

/** Discriminated union for report table rows */
export type DisplayRow =
  | { kind: "group"; depth: number; label: string; count: number; totals: Record<string, ReportTotalValue> }
  | { kind: "data"; depth: number; item: JsonObject }
  | { kind: "subtotal"; depth: number; totals: Record<string, ReportTotalValue> }
  | { kind: "footer"; totals: Record<string, ReportTotalValue> }

// ── Report Status ───────────────────────────────────────────────────────

export type ReportStatus = "idle" | "loading" | "done" | "empty" | "error" | "export-only"

export interface MoneySources { amountType?: "raw"; currencyId: string; currencyCode: string; decimalPlaces: string }
export interface MoneyTotal { amount: string; currencyId: string; currencyCode: string; decimalPlaces: number }
export type ReportTotalValue = number | MoneyTotal[]

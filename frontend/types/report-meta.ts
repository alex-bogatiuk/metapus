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
}

export interface ReportFilterDef {
    key: string
    type: "date" | "period" | "reference" | "boolean" | "enum" | "string"
    label: string
    required?: boolean
    /** Entity name for reference picker (e.g. "warehouse") */
    ref?: string
    /** Allow multiple values */
    multi?: boolean
    /** Default value */
    default?: unknown
}

export interface ReportColumnDef {
    key: string
    label: string
    type: "string" | "quantity" | "money" | "date" | "reference" | "boolean"
    align?: "left" | "center" | "right"
    sortable?: boolean
    defaultHidden?: boolean
    format?: "number" | "currency" | "percent"
}

export interface ReportGroupByDef {
    key: string
    label: string
    defaultActive?: boolean
}

export interface ReportTotalDef {
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
    | { kind: "group";    depth: number; label: string; count: number; subtotals: Record<string, number> }
    | { kind: "data";     depth: number; item: Record<string, unknown> }
    | { kind: "subtotal"; depth: number; totals: Record<string, number> }
    | { kind: "footer";   totals: Record<string, number> }

// ── Report Status ───────────────────────────────────────────────────────

export type ReportStatus = "idle" | "loading" | "done" | "empty" | "error"

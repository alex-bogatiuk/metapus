"use client"

/**
 * useReportPage — orchestration hook for metadata-driven report pages.
 *
 * Encapsulates:
 *  - Metadata loading from GET /reports/{key}/metadata
 *  - Filter state management (URL-backed for shareability) [#15]
 *  - Report execution (idle → loading → done/empty/error)
 *  - Client-side grouping & sorting (no re-fetch needed)
 *  - Column visibility (via useVisibleColumns pattern)
 *  - Export URLs
 *  - Report Variants — save/load named presets [#14]
 *  - Drill-down — row click → detail data [#13]
 *  - View mode toggle: table / chart [#16]
 *  - Auto-generate on URL open (when filters present) [#15]
 *  - Copy shareable link [#15]
 *
 * Usage:
 * ```tsx
 * const report = useReportPage("stock-balance")
 * <ReportPage report={report} />
 * ```
 */

import { useState, useCallback, useMemo, useEffect, useRef } from "react"
import { useSearchParams, useRouter, usePathname } from "next/navigation"
import { apiFetch } from "@/lib/api"
import { buildDisplayRows, getDefaultGroupByKeys, sortItems } from "@/lib/report-grouping"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import type {
    ReportMeta,
    ReportColumnDef,
    ReportStatus,
    DisplayRow,
    FieldTreeNode,
} from "@/types/report-meta"

// ── Helpers for dynamic column generation ───────────────────────────────

/** Recursively find a FieldTreeNode by its dot-separated key. */
function findFieldNode(nodes: FieldTreeNode[], key: string): FieldTreeNode | null {
    for (const node of nodes) {
        if (node.key === key) return node
        if (node.children) {
            const found = findFieldNode(node.children, key)
            if (found) return found
        }
    }
    return null
}

/** Build a human-readable label like "Товар → Артикул" from a nested field key. */
function buildFieldLabel(node: FieldTreeNode, fullKey: string): string {
    const parts = fullKey.split(".")
    if (parts.length <= 1) return node.label
    // Use the last part's label from the node, prefix not needed for simple cases
    return node.label
}

/** Map field tree node type to ReportColumnDef type. */
function mapFieldType(type?: string): ReportColumnDef["type"] {
    switch (type) {
        case "quantity": return "quantity"
        case "money": return "money"
        case "date": case "datetime": return "date"
        case "boolean": return "boolean"
        case "ref": return "reference"
        default: return "string"
    }
}

// ── Types ───────────────────────────────────────────────────────────────

/** Filter values are stored as key → primitive/array. */
export type ReportFilterValues = Record<string, unknown>

/** Generic report result with items array. */
interface ReportResult {
    items: Record<string, unknown>[]
    totalItems?: number
    [key: string]: unknown
}

/** QueryRequest body for POST-based dataset execution. */
interface QueryRequest {
    dataset: string
    select?: string[]
    groupBy?: string[]
    orderBy?: string
    orderDir?: string
    filters?: Record<string, unknown>
    limit?: number
    offset?: number
}

/** Server-side grouped response from /grouped endpoint. */
interface GroupedResponse {
    rows: DisplayRow[]
    totalItems: number
}

/**
 * Threshold for client vs server grouping.
 * Below this: buildDisplayRows() runs in the browser (~50ms).
 * Above this: we delegate to GET /reports/{key}/grouped.
 * Above 50000: export-only mode (no interactive table).
 */
const CLIENT_GROUPING_THRESHOLD = 5000
const MAX_INTERACTIVE_ROWS = 50000

/** Named report variant (saved preset). */
export interface ReportVariant {
    name: string
    filters: ReportFilterValues
    groupBy: string[]
    visibleColumns: string[]
    sortColumn: string | null
    sortDirection: "asc" | "desc"
}

/** View mode for report display. */
export type ReportViewMode = "table" | "chart"

export interface UseReportPageReturn {
    // Metadata
    meta: ReportMeta | null
    metaLoading: boolean

    // Status
    status: ReportStatus
    error: string | null

    // Data
    items: Record<string, unknown>[]
    displayRows: DisplayRow[]
    totalItems: number

    // Filters
    filterValues: ReportFilterValues
    setFilterValue: (key: string, value: unknown) => void
    resetFilters: () => void

    // Actions
    generate: () => Promise<void>
    exportReport: (format: string) => void

    // Grouping
    activeGroupBy: string[]
    toggleGroupBy: (key: string) => void

    // Sorting (client-side)
    sortColumn: string | null
    sortDirection: "asc" | "desc"
    handleSort: (key: string) => void

    // Columns
    visibleColumnKeys: string[]
    toggleColumn: (key: string) => void
    resetColumns: () => void

    // Report result extras (totalQuantity, totalCost, etc.)
    resultExtras: Record<string, unknown>

    // ── Wave 3: Report Variants (#14) ────────────────────────────────
    variants: ReportVariant[]
    activeVariantName: string | null
    saveVariant: (name: string) => void
    loadVariant: (name: string) => void
    deleteVariant: (name: string) => void

    // ── Wave 3: Drill-Down (#13) ─────────────────────────────────────
    selectedRow: Record<string, unknown> | null
    selectRow: (row: Record<string, unknown> | null) => void

    // ── Wave 3: URL Sharing (#15) ────────────────────────────────────
    copyShareLink: () => void

    // ── Wave 3: View Mode (#16) ──────────────────────────────────────
    viewMode: ReportViewMode
    setViewMode: (mode: ReportViewMode) => void

    // ── Query Engine: Field Selection ────────────────────────────────
    /** Tree of available fields from Auto-Discovery (null = legacy mode). */
    availableFields: FieldTreeNode[] | null
    /** Currently selected field paths (dot-separated). */
    selectedFields: string[]
    /** Toggle a field path in the selection. */
    toggleField: (fieldKey: string) => void
    /** Effective columns (static + dynamically generated for extra selected fields). */
    effectiveColumns: ReportColumnDef[]

    // ── Stale State ──────────────────────────────────────────────────
    /** True if filters or selected fields changed since last generate. */
    isDirty: boolean
}

// ── Hook ────────────────────────────────────────────────────────────────

export function useReportPage(reportKey: string): UseReportPageReturn {
    const router = useRouter()
    const pathname = usePathname()
    const searchParams = useSearchParams()

    // ── Metadata ─────────────────────────────────────────────────────
    const [meta, setMeta] = useState<ReportMeta | null>(null)
    const [metaLoading, setMetaLoading] = useState(true)

    const [isDirty, setIsDirty] = useState(false)

    // Hoisted above metadata useEffect that references these setters
    const [activeGroupBy, setActiveGroupBy] = useState<string[]>([])
    const [visibleColumnKeys, setVisibleColumnKeys] = useState<string[]>([])
    const [sortColumn, setSortColumn] = useState<string | null>(null)
    const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc")
    const sortInitialized = useRef(false)

    // ── Query Engine: Selected Fields ────────────────────────────────
    const [selectedFields, setSelectedFields] = useState<string[]>([])

    const toggleField = useCallback((fieldKey: string) => {
        // Convert field key (dot-separated) to column key (__-separated)
        const colKey = fieldKey.replaceAll(".", "__")
        setSelectedFields((prev) => {
            const isRemoving = prev.includes(fieldKey)
            const next = isRemoving
                ? prev.filter((k) => k !== fieldKey)
                : [...prev, fieldKey]

            // Auto-sync visible column keys
            setVisibleColumnKeys((cols) => {
                if (isRemoving) {
                    return cols.filter((k) => k !== colKey)
                }
                return cols.includes(colKey) ? cols : [...cols, colKey]
            })

            return next
        })
        setIsDirty(true)
    }, [])

    const availableFields = useMemo(() => meta?.availableFields ?? null, [meta])
    const isQueryEngine = !!availableFields && availableFields.length > 0

    // Build effective columns: merge static meta.columns with dynamic columns
    // for fields that are selected but not in the static columns list.
    const effectiveColumns = useMemo(() => {
        if (!meta) return []
        const staticKeys = new Set(meta.columns.map((c) => c.key))
        const dynamicCols: typeof meta.columns = []

        for (const fieldKey of selectedFields) {
            const colKey = fieldKey.replaceAll(".", "__")
            if (!staticKeys.has(colKey)) {
                // Generate a column definition from the field tree node
                const node = findFieldNode(availableFields ?? [], fieldKey)
                dynamicCols.push({
                    key: colKey,
                    label: node ? buildFieldLabel(node, fieldKey) : colKey,
                    type: mapFieldType(node?.type),
                    sortable: node?.sortable,
                    align: (node?.type === "quantity" || node?.type === "money" || node?.type === "number") ? "right" : undefined,
                })
            }
        }

        return [...meta.columns, ...dynamicCols]
    }, [meta, selectedFields, availableFields])

    useEffect(() => {
        let cancelled = false
        // metaLoading is initialized as `true`, no need to set it again here
        apiFetch<ReportMeta>(`/reports/${reportKey}/metadata`)
            .then((data) => {
                if (!cancelled) {
                    setMeta(data)
                    setActiveGroupBy(getDefaultGroupByKeys(data.groupBy))
                    const defaultKeys = data.columns
                        .filter((c) => !c.defaultHidden)
                        .map((c) => c.key)
                    const saved = useUserPrefsStore.getState().listColumns[`report:${reportKey}`]
                    setVisibleColumnKeys(saved && saved.length > 0 ? saved : defaultKeys)
                    // Initialize default sort from metadata (avoids a separate effect)
                    if (data.defaultSort && !sortInitialized.current) {
                        sortInitialized.current = true
                        setSortColumn(data.defaultSort.column)
                        setSortDirection(data.defaultSort.direction)
                    }
                    // Initialize selected fields from availableFields (Query Engine mode)
                    // defaultKeys use __ (column keys), but selectedFields stores dot-separated paths
                    if (data.availableFields && data.availableFields.length > 0) {
                        setSelectedFields(defaultKeys.map((k) => k.replaceAll("__", ".")))
                    }
                    // Apply default filter values from metadata (only if not overridden by URL)
                    setFilterValues((prev) => {
                        const merged = { ...prev }
                        for (const f of data.filters) {
                            if (f.default !== undefined && f.default !== null && !(f.key in merged)) {
                                merged[f.key] = f.default
                            }
                        }
                        return merged
                    })
                }
            })
            .catch(() => { /* metadata load failure handled gracefully */ })
            .finally(() => { if (!cancelled) setMetaLoading(false) })
        return () => { cancelled = true }
    }, [reportKey])

    // ── Filter values (URL-backed) [#15] ─────────────────────────────
    const [filterValues, setFilterValues] = useState<ReportFilterValues>(() => {
        const initial: ReportFilterValues = {}
        searchParams.forEach((value, key) => {
            if (key.startsWith("f.")) {
                const filterKey = key.slice(2)
                const existing = initial[filterKey]
                if (existing !== undefined) {
                    initial[filterKey] = Array.isArray(existing)
                        ? [...existing, value]
                        : [existing, value]
                } else {
                    initial[filterKey] = value
                }
            }
        })
        return initial
    })

    const setFilterValue = useCallback((key: string, value: unknown) => {
        setFilterValues((prev) => {
            if (value === undefined || value === null || value === "") {
                const next = { ...prev }
                delete next[key]
                return next
            }
            return { ...prev, [key]: value }
        })
        setIsDirty(true)
    }, [])

    const resetFilters = useCallback(() => {
        setFilterValues({})
        setIsDirty(true)
    }, [])

    // Sync filters to URL
    const syncToUrl = useCallback((filters: ReportFilterValues) => {
        const params = new URLSearchParams()
        for (const [key, value] of Object.entries(filters)) {
            if (value === undefined || value === null) continue
            if (Array.isArray(value)) {
                for (const v of value) params.append(`f.${key}`, String(v))
            } else {
                params.set(`f.${key}`, String(value))
            }
        }
        const qs = params.toString()
        router.replace(`${pathname}${qs ? `?${qs}` : ""}`, { scroll: false })
    }, [router, pathname])

    // ── Report execution ─────────────────────────────────────────────
    const [status, setStatus] = useState<ReportStatus>("idle")
    const [error, setError] = useState<string | null>(null)
    const [items, setItems] = useState<Record<string, unknown>[]>([])
    const [resultExtras, setResultExtras] = useState<Record<string, unknown>>({})

    const generate = useCallback(async () => {
        if (!meta) return

        setIsDirty(false)
        setStatus("loading")
        setError(null)
        setServerDisplayRows(null)
        syncToUrl(filterValues)

        try {
            let result: ReportResult

            if (isQueryEngine) {
                // Query Engine mode: POST with QueryRequest body
                // selectedFields already stores dot-separated paths (e.g. "warehouse_id.name")
                const selectPaths = selectedFields.length > 0
                    ? selectedFields
                    : undefined
                const body: QueryRequest = {
                    dataset: reportKey,
                    select: selectPaths,
                    orderBy: sortColumn ? sortColumn.replace(/__/g, ".") : undefined,
                    orderDir: sortDirection,
                    filters: filterValues,
                    limit: MAX_INTERACTIVE_ROWS + 1,
                }
                result = await apiFetch<ReportResult>(
                    `/reports/${reportKey}`,
                    { method: "POST", body: JSON.stringify(body) }
                )
            } else {
                // Legacy mode: GET with query params
                const params = new URLSearchParams()
                for (const [key, value] of Object.entries(filterValues)) {
                    if (value === undefined || value === null) continue
                    if (Array.isArray(value)) {
                        for (const v of value) params.append(key, String(v))
                    } else {
                        params.set(key, String(value))
                    }
                }
                params.set("limit", String(MAX_INTERACTIVE_ROWS + 1))
                const qs = params.toString()
                result = await apiFetch<ReportResult>(
                    `/reports/${reportKey}${qs ? `?${qs}` : ""}`
                )
            }

            const reportItems = result.items ?? []
            const totalCount = result.totalItems ?? reportItems.length

            if (totalCount > MAX_INTERACTIVE_ROWS) {
                setItems([])
                setStatus("export-only")
            } else {
                setItems(reportItems)
                setStatus(reportItems.length > 0 ? "done" : "empty")
            }

            const { items: _, ...extras } = result
            setResultExtras(extras)
        } catch (e) {
            const msg = e instanceof Error ? e.message : "Ошибка формирования отчёта"
            setError(msg)
            setStatus("error")
        }
    }, [reportKey, filterValues, syncToUrl, isQueryEngine, selectedFields, sortColumn, sortDirection])

    // ── Auto-generate on URL open [#15] ──────────────────────────────
    // If URL has filter params (f.*), auto-generate report on mount.
    const autoGenerated = useRef(false)
    useEffect(() => {
        if (autoGenerated.current || metaLoading || !meta) return
        const hasUrlFilters = Array.from(searchParams.keys()).some((k) => k.startsWith("f."))
        if (hasUrlFilters) {
            autoGenerated.current = true
            // Defer to avoid synchronous setState inside effect body
            queueMicrotask(() => generate())
        }
    }, [metaLoading, meta, searchParams, generate])

    // ── Copy shareable link [#15] ────────────────────────────────────
    const copyShareLink = useCallback(() => {
        const params = new URLSearchParams()
        for (const [key, value] of Object.entries(filterValues)) {
            if (value === undefined || value === null) continue
            if (Array.isArray(value)) {
                for (const v of value) params.append(`f.${key}`, String(v))
            } else {
                params.set(`f.${key}`, String(value))
            }
        }
        const qs = params.toString()
        const url = `${window.location.origin}${pathname}${qs ? `?${qs}` : ""}`
        navigator.clipboard.writeText(url).catch(() => {
            // Fallback: prompt
            window.prompt("Ссылка на отчёт:", url)
        })
    }, [filterValues, pathname])

    // ── Grouping (client-side) ───────────────────────────────────────

    const toggleGroupBy = useCallback((key: string) => {
        setActiveGroupBy((prev) =>
            prev.includes(key)
                ? prev.filter((k) => k !== key)
                : [...prev, key]
        )
    }, [])

    // ── Sorting (client-side) ────────────────────────────────────────

    const handleSort = useCallback((key: string) => {
        setSortColumn((prev) => {
            if (prev === key) {
                setSortDirection((d) => (d === "asc" ? "desc" : "asc"))
                return key
            }
            setSortDirection("asc")
            return key
        })
    }, [])

    // ── Column visibility ────────────────────────────────────────────
    const setListColumns = useUserPrefsStore((s) => s.setListColumns)

    const toggleColumn = useCallback((key: string) => {
        setVisibleColumnKeys((prev) => {
            const next = prev.includes(key)
                ? prev.filter((k) => k !== key)
                : [...prev, key]
            setListColumns(`report:${reportKey}`, next)
            return next
        })
    }, [reportKey, setListColumns])

    const resetColumns = useCallback(() => {
        if (!meta) return
        const defaults = meta.columns.filter((c) => !c.defaultHidden).map((c) => c.key)
        setVisibleColumnKeys(defaults)
        setListColumns(`report:${reportKey}`, defaults)
    }, [meta, reportKey, setListColumns])

    // ── Report Variants [#14] ────────────────────────────────────────
    // Variants are stored in user prefs under `report:{key}:variants` as JSON.
    const variantsKey = `report:${reportKey}:variants`
    const [variants, setVariants] = useState<ReportVariant[]>(() => {
        try {
            const raw = useUserPrefsStore.getState().listColumns[variantsKey]
            if (raw && typeof raw === "string") {
                return JSON.parse(raw as unknown as string) as ReportVariant[]
            }
        } catch { /* ignore */ }
        return []
    })
    const [activeVariantName, setActiveVariantName] = useState<string | null>(null)

    const saveVariant = useCallback((name: string) => {
        const variant: ReportVariant = {
            name,
            filters: filterValues,
            groupBy: activeGroupBy,
            visibleColumns: visibleColumnKeys,
            sortColumn,
            sortDirection,
        }
        setVariants((prev) => {
            const next = prev.filter((v) => v.name !== name)
            next.push(variant)
            // Persist as JSON string in listColumns store
            setListColumns(variantsKey, [JSON.stringify(next)])
            return next
        })
        setActiveVariantName(name)
    }, [filterValues, activeGroupBy, visibleColumnKeys, sortColumn, sortDirection, variantsKey, setListColumns])

    const loadVariant = useCallback((name: string) => {
        const variant = variants.find((v) => v.name === name)
        if (!variant) return
        setFilterValues(variant.filters)
        setActiveGroupBy(variant.groupBy)
        setVisibleColumnKeys(variant.visibleColumns)
        setSortColumn(variant.sortColumn)
        setSortDirection(variant.sortDirection)
        setActiveVariantName(name)
    }, [variants])

    const deleteVariant = useCallback((name: string) => {
        setVariants((prev) => {
            const next = prev.filter((v) => v.name !== name)
            setListColumns(variantsKey, [JSON.stringify(next)])
            return next
        })
        if (activeVariantName === name) {
            setActiveVariantName(null)
        }
    }, [activeVariantName, variantsKey, setListColumns])

    // ── Drill-Down [#13] ─────────────────────────────────────────────
    const [selectedRow, setSelectedRow] = useState<Record<string, unknown> | null>(null)

    const selectRow = useCallback((row: Record<string, unknown> | null) => {
        setSelectedRow(row)
    }, [])

    // ── View Mode [#16] ──────────────────────────────────────────────
    const [viewMode, setViewMode] = useState<ReportViewMode>("table")

    // ── Server-grouped rows (Wave 4) ─────────────────────────────────
    const [serverDisplayRows, setServerDisplayRows] = useState<DisplayRow[] | null>(null)
    const serverGroupingInFlight = useRef(false)

    // Fetch server-side grouped rows when items > threshold
    const fetchGroupedRows = useCallback(async () => {
        if (items.length <= CLIENT_GROUPING_THRESHOLD) return
        if (serverGroupingInFlight.current) return
        serverGroupingInFlight.current = true

        try {
            const params = new URLSearchParams()
            for (const [key, value] of Object.entries(filterValues)) {
                if (value === undefined || value === null) continue
                if (Array.isArray(value)) {
                    for (const v of value) params.append(key, String(v))
                } else {
                    params.set(key, String(value))
                }
            }
            for (const g of activeGroupBy) {
                params.append("groupBy", g)
            }
            if (sortColumn) {
                params.set("sortBy", sortColumn.replace(/__/g, "."))
                params.set("sortDir", sortDirection)
            }

            const qs = params.toString()
            const result = await apiFetch<GroupedResponse>(
                `/reports/${reportKey}/grouped${qs ? `?${qs}` : ""}`
            )
            setServerDisplayRows(result.rows)
        } catch {
            // Fallback: if server grouping fails, use client-side
            setServerDisplayRows(null)
        } finally {
            serverGroupingInFlight.current = false
        }
    }, [reportKey, filterValues, activeGroupBy, sortColumn, sortDirection, items.length])

    // Trigger server grouping when groupBy/sort changes and items > threshold
    useEffect(() => {
        if (items.length > CLIENT_GROUPING_THRESHOLD && status === "done") {
            fetchGroupedRows()
        } else {
            setServerDisplayRows(null)
        }
    }, [items.length, status, activeGroupBy, sortColumn, sortDirection]) // eslint-disable-line react-hooks/exhaustive-deps

    // ── Computed: sorted + grouped display rows (adaptive) ───────────
    const displayRows = useMemo(() => {
        if (items.length === 0) return []

        // Server-side grouping: use pre-built rows from /grouped
        if (items.length > CLIENT_GROUPING_THRESHOLD && serverDisplayRows) {
            return serverDisplayRows
        }

        // Client-side grouping (≤5000 rows)
        let sorted = items
        if (sortColumn) {
            sorted = sortItems(items, sortColumn, sortDirection)
        }

        const groupByDataKeys = activeGroupBy.map((groupKey) => {
            // First check if a dynamic column explicitly matches the path (e.g. warehouse_id)
            const dynamicCol = effectiveColumns.find((c) => c.key === groupKey)
            if (dynamicCol && dynamicCol.key) return dynamicCol.key
            
            // If it's a base column, the backend appends __name to references
            const baseCol = meta?.columns.find((c) => c.key === groupKey || c.key === `${groupKey}__name`)
            return baseCol ? baseCol.key : groupKey
        })

        return buildDisplayRows(sorted, groupByDataKeys, meta?.totals ?? [])
    }, [items, sortColumn, sortDirection, activeGroupBy, meta?.totals, serverDisplayRows, effectiveColumns, meta?.columns])

    return {
        meta,
        metaLoading,
        status,
        error,
        items,
        displayRows,
        totalItems: items.length,
        filterValues,
        setFilterValue,
        resetFilters,
        generate,
        exportReport: (format: string) => {
            const params = new URLSearchParams()
            for (const [key, value] of Object.entries(filterValues)) {
                if (value === undefined || value === null) continue
                if (Array.isArray(value)) {
                    for (const v of value) params.append(key, String(v))
                } else {
                    params.set(key, String(value))
                }
            }
            params.set("format", format)
            const apiBase = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"
            window.open(`${apiBase}/reports/${reportKey}/export?${params.toString()}`, "_blank")
        },
        activeGroupBy,
        toggleGroupBy,
        sortColumn,
        sortDirection,
        handleSort,
        visibleColumnKeys,
        toggleColumn,
        resetColumns,
        resultExtras,
        // Wave 3
        variants,
        activeVariantName,
        saveVariant,
        loadVariant,
        deleteVariant,
        selectedRow,
        selectRow,
        copyShareLink,
        viewMode,
        setViewMode,
        // Query Engine
        availableFields,
        selectedFields,
        toggleField,
        effectiveColumns,
        isDirty,
    }
}

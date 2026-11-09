"use client"

/**
 * useReportPage — orchestration hook for metadata-driven report pages.
 *
 * Encapsulates:
 *  - Metadata loading from GET /reports/{key}/metadata
 *  - Filter state management (URL-backed for shareability)
 *  - Report execution (idle → loading → done/empty/error)
 *  - Client-side grouping & sorting (no re-fetch needed)
 *  - Column visibility (via useVisibleColumns pattern)
 *  - Export URLs
 *  - Report Variants — save/load named presets
 *  - Drill-down — row click → detail data
 *  - View mode toggle: table / chart
 *  - Auto-generate on URL open (when filters present)
 *  - Copy shareable link
 *
 * Usage:
 * ```tsx
 * const report = useReportPage("document-journal")
 * <ReportPage report={report} />
 * ```
 */

import type { FilterFieldMeta } from "@/components/shared/filter-config-dialog"
import { useHasTabCache, useTabState } from "@/hooks/useTabState"
import { api, apiFetch, apiFetchBlob } from "@/lib/api"
import { buildFilterItems, type FilterValues } from "@/lib/filter-utils"
import { DatasetQueryRequestSchema } from "@/lib/generated/http-contract-schemas"
import type { JsonObject, JsonValue } from "@/lib/json-value"
import { reportMetaToFilterFieldsMeta } from "@/lib/report-filter-adapter"
import { buildDisplayRows, getDefaultGroupByKeys, sortItems } from "@/lib/report-grouping"
import { validateReportMoney } from "@/lib/report-money"
import { reportMetadataSchema } from "@/lib/schemas/report-metadata"
import { groupedReportResponseSchema, reportResultSchema } from "@/lib/schemas/report-results"
import { restoreVariantState } from "@/lib/schemas/report-variants"
import { createTabStateKey } from "@/stores/useTabStateStore"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import type { DatasetQueryRequest } from "@/types/generated/http-contracts"
import type {
  DisplayRow,
  FieldTreeNode,
  ReportColumnDef,
  ReportMeta,
  ReportStatus,
} from "@/types/report-meta"
import { usePathname, useRouter, useSearchParams } from "next/navigation"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { toast } from "sonner"

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

/** Build a human-readable label like "Товар.Артикул" from a nested field key. */
function buildFieldLabel(nodes: FieldTreeNode[], fullKey: string): string {
    const parts = fullKey.split(".")
    if (parts.length <= 1) {
        const node = findFieldNode(nodes, fullKey)
        return node?.label ?? fullKey
    }

    const labels: string[] = []
    let currentPath = ""
    for (let i = 0; i < parts.length; i++) {
        currentPath = currentPath ? `${currentPath}.${parts[i]}` : parts[i]
        const node = findFieldNode(nodes, currentPath)
        if (node) {
            labels.push(node.label)
        } else {
            labels.push(parts[i])
        }
    }

    return labels.join(".")
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
export type ReportFilterValues = Record<string, JsonValue>

/** Generic report result with items array. */

/**
 * Threshold for client vs server grouping.
 * Below this: buildDisplayRows() runs in the browser (~50ms).
 * Above this: we delegate to POST /reports/{key}/grouped.
 * Above 50000: export-only mode (no interactive table).
 */
const CLIENT_GROUPING_THRESHOLD = 5000
const MAX_INTERACTIVE_ROWS = 50000

import type { ReportVariant } from "@/types/report-variant"

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
    items: JsonObject[]
    displayRows: DisplayRow[]
    totalItems: number

    // Filters
    filterValues: ReportFilterValues
    setFilterValue: (key: string, value?: JsonValue) => void
    resetFilters: () => void

    // Actions
    generate: () => Promise<void>
    exportReport: () => Promise<void> | void

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
    reorderColumn: (fromIndex: number, toIndex: number) => void

    // Report result extras (totals, etc.)
    resultExtras: JsonObject

    // ── Report Variants ────────────────────────────────
    variants: ReportVariant[]
    activeVariantId: string | null
    saveVariant: (name: string, visibility: import("@/types/report-variant").VariantVisibility, isDefault: boolean) => Promise<void>
    updateVariant: (id: string, name: string, visibility: import("@/types/report-variant").VariantVisibility, isDefault: boolean) => Promise<void>
    loadVariant: (id: string) => void
    deleteVariant: (id: string) => Promise<void>

    // ── Cell Selection ────────────────────────────────────────────────
    selectionRange: { start: { r: number, c: number }, end: { r: number, c: number } } | null
    isDraggingSelection: boolean
    onSelectionStart: (r: number, c: number) => void
    onSelectionMove: (r: number, c: number) => void
    onSelectionEnd: () => void
    resetSelection: () => void

    // ── URL Sharing ────────────────────────────────────
    copyShareLink: () => void

    // ── View Mode ──────────────────────────────────────
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

    // ── Advanced Filters (FilterSidebar integration) ─────────────────
    /** FilterFieldMeta[] for FilterSidebar (computed from report meta). */
    filterFieldsMeta: FilterFieldMeta[]
    /** Advanced filter values from FilterSidebar. */
    advancedFilterValues: FilterValues
    /** Callback for FilterSidebar's onFilterValuesChange. */
    onAdvancedFilterValuesChange: (values: FilterValues) => void

    // ── Stale State ──────────────────────────────────────────────────
    /** True if filters or selected fields changed since last generate. */
    isDirty: boolean
}

const tabKeys = {
    meta: createTabStateKey<ReportMeta | null>("report.meta"),
    metaLoading: createTabStateKey<boolean>("report.metaLoading"),
    isDirty: createTabStateKey<boolean>("report.isDirty"),
    activeGroupBy: createTabStateKey<string[]>("report.activeGroupBy"),
    visibleColumnKeys: createTabStateKey<string[]>("report.visibleColumnKeys"),
    sortColumn: createTabStateKey<string | null>("report.sortColumn"),
    sortDirection: createTabStateKey<"asc" | "desc">("report.sortDirection"),
    selectedFields: createTabStateKey<string[]>("report.selectedFields"),
    advancedFilterValues: createTabStateKey<FilterValues>("report.advancedFilterValues"),
    filterValues: createTabStateKey<ReportFilterValues>("report.filterValues"),
    status: createTabStateKey<ReportStatus>("report.status"),
    error: createTabStateKey<string | null>("report.error"),
    items: createTabStateKey<JsonObject[]>("report.items"),
    resultExtras: createTabStateKey<JsonObject>("report.resultExtras"),
    variants: createTabStateKey<ReportVariant[]>("report.variants"),
    activeVariantId: createTabStateKey<string | null>("report.activeVariantId"),
    viewMode: createTabStateKey<ReportViewMode>("report.viewMode"),
    serverDisplayRows: createTabStateKey<DisplayRow[] | null>("report.serverDisplayRows"),
    generatedQuery: createTabStateKey<DatasetQueryRequest | null>("report.generatedQuery"),
}

// ── Hook ────────────────────────────────────────────────────────────────

export function useReportPage(reportKey: string): UseReportPageReturn {
    const router = useRouter()
    const pathname = usePathname()
    const searchParams = useSearchParams()

    // ── Metadata ─────────────────────────────────────────────────────
    const hasCachedMeta = useHasTabCache(tabKeys.meta)
    const [meta, setMeta] = useTabState(tabKeys.meta, null)
    const [metaLoading, setMetaLoading] = useTabState(tabKeys.metaLoading, !hasCachedMeta)

    const [isDirty, setIsDirty] = useTabState(tabKeys.isDirty, false)

    // Hoisted above metadata useEffect that references these setters
    const [activeGroupBy, setActiveGroupBy] = useTabState(tabKeys.activeGroupBy, [])
    const [visibleColumnKeys, setVisibleColumnKeys] = useTabState(tabKeys.visibleColumnKeys, [])
    const [sortColumn, setSortColumn] = useTabState(tabKeys.sortColumn, null)
    const [sortDirection, setSortDirection] = useTabState(tabKeys.sortDirection, "asc")
    const sortInitialized = useRef(hasCachedMeta)

    // ── Query Engine: Selected Fields ────────────────────────────────
    const [selectedFields, setSelectedFields] = useTabState(tabKeys.selectedFields, [])

    const availableFields = useMemo(() => meta?.availableFields ?? null, [meta])


    const toggleField = useCallback((fieldKey: string) => {
        // For root ref fields (e.g. "wallet_id"), toggling means toggling
        // "wallet_id.name" — the default human-readable representation.
        // This matches 1C behavior: selecting a ref field = show its Представление().
        const node = findFieldNode(availableFields ?? [], fieldKey)
        const effectiveFieldKey = (node?.type === "ref" && !fieldKey.includes("."))
            ? fieldKey + ".name"
            : fieldKey

        const colKey = effectiveFieldKey.replaceAll(".", "__")
        setSelectedFields((prev) => {
            const isRemoving = prev.includes(effectiveFieldKey)
            const next = isRemoving
                ? prev.filter((k) => k !== effectiveFieldKey)
                : [...prev, effectiveFieldKey]

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
    }, [availableFields, setSelectedFields, setVisibleColumnKeys, setIsDirty])

    // ── Advanced Filters (FilterSidebar integration) ─────────────────
    const [advancedFilterValues, setAdvancedFilterValues] = useTabState(tabKeys.advancedFilterValues, {})

    const filterFieldsMeta = useMemo<FilterFieldMeta[]>(() =>
        meta ? reportMetaToFilterFieldsMeta(meta) : [],
        [meta]
    )

    const onAdvancedFilterValuesChange = useCallback((values: FilterValues) => {
        setAdvancedFilterValues(values)
        setIsDirty(true)
    }, [setAdvancedFilterValues, setIsDirty])

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
                let type = mapFieldType(node?.type)
                let refIdKey = type === "reference" ? colKey : undefined
                let refRoute = node?.refRoute

                // If this is a ".name" field of a reference, map it as a reference
                // so the user can double-click it to open the entity.
                if (fieldKey.endsWith(".name")) {
                    const parentKey = fieldKey.slice(0, -5)
                    const parentNode = findFieldNode(availableFields ?? [], parentKey)
                    if (parentNode?.type === "ref") {
                        type = "reference"
                        refIdKey = parentKey.replaceAll(".", "__")
                        refRoute = parentNode.refRoute
                    }
                }

                dynamicCols.push({
                    key: colKey,
                    label: buildFieldLabel(availableFields ?? [], fieldKey),
                    type: type,
                    sortable: node?.sortable,
                    align: (node?.type === "quantity" || node?.type === "money" || node?.type === "number") ? "right" : undefined,
                    refIdKey: refIdKey,
                    refRoute: refRoute,
                })
            }
        }

        return [...meta.columns, ...dynamicCols]
    }, [meta, selectedFields, availableFields])

    // ── Filter values (URL-backed) ─────────────────────────────
    const initialFilters = useMemo(() => {
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
    }, [searchParams])
    const [filterValues, setFilterValues] = useTabState(tabKeys.filterValues, initialFilters)


    const metaInitialized = useRef(hasCachedMeta)

    useEffect(() => {
        if (metaInitialized.current) return

        let cancelled = false
        // metaLoading is initialized as `true`, no need to set it again here
        apiFetch<ReportMeta>(`/reports/${reportKey}/metadata`)
            .then(data => reportMetadataSchema.parse(data))
            .then((data) => {
                if (!cancelled) {
                    metaInitialized.current = true
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

    }, [reportKey, setMeta, setActiveGroupBy, setVisibleColumnKeys, setSortColumn, setSortDirection, setSelectedFields, setMetaLoading, setFilterValues])

    const setFilterValue = useCallback((key: string, value?: JsonValue) => {
        setFilterValues((prev) => {
            if (value === undefined || value === null || value === "") {
                const next = { ...prev }
                delete next[key]
                return next
            }
            return { ...prev, [key]: value }
        })
        setIsDirty(true)
    }, [setFilterValues, setIsDirty])

    const resetFilters = useCallback(() => {
        setFilterValues({})
        setIsDirty(true)
    }, [setFilterValues, setIsDirty])

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
    const [status, setStatus] = useTabState(tabKeys.status, "idle")
    const [error, setError] = useTabState(tabKeys.error, null)
    const [items, setItems] = useTabState(tabKeys.items, [])
    const [resultExtras, setResultExtras] = useTabState(tabKeys.resultExtras, {})
    const [generatedQuery, setGeneratedQuery] = useTabState(tabKeys.generatedQuery, null)
    const execution = useRef<AbortController | null>(null)
    const autoGenerated = useRef(useHasTabCache(tabKeys.items))
    useEffect(() => () => execution.current?.abort(), [])

    const [serverDisplayRows, setServerDisplayRows] = useTabState(tabKeys.serverDisplayRows, null)

    const [selectionRange, setSelectionRange] = useState<{ start: { r: number, c: number }, end: { r: number, c: number } } | null>(null)

    const generate = useCallback(async () => {
        if (!meta) return
        autoGenerated.current = true

        execution.current?.abort()
        const controller = new AbortController()
        execution.current = controller

        setIsDirty(false)
        setStatus("loading")
        setError(null)
        setServerDisplayRows(null)
        // Clear selection on new generation
        setSelectionRange(null)
        syncToUrl(filterValues)

        try {
            // POST with QueryRequest body
            // selectedFields stores dot-separated paths (e.g. "wallet_id.name")
            // For ref .name fields, also include the parent UUID field for dblclick navigation.
            let selectPaths: string[] | undefined
            if (selectedFields.length > 0) {
                const expanded = new Set<string>()
                for (const fieldKey of selectedFields) {
                    expanded.add(fieldKey)
                    // If field ends with ".name" and the parent is a ref field,
                    // also include the UUID field for navigation
                    if (fieldKey.endsWith(".name")) {
                        const parentKey = fieldKey.slice(0, -5) // remove ".name"
                        const parentNode = findFieldNode(availableFields ?? [], parentKey)
                        if (parentNode?.type === "ref") {
                            expanded.add(parentKey) // add UUID field
                        }
                    }
                }
                selectPaths = Array.from(expanded)
            }
            const body = DatasetQueryRequestSchema.parse({
                dataset: reportKey,
                select: selectPaths,
                orderBy: sortColumn ? sortColumn.replace(/__/g, ".") : undefined,
                orderDir: sortDirection,
                filters: filterValues,
                advancedFilters: buildFilterItems(advancedFilterValues, filterFieldsMeta),
                limit: MAX_INTERACTIVE_ROWS + 1,
            })
            const result = reportResultSchema.parse(await apiFetch<JsonObject>(
                `/reports/${reportKey}`,
                { method: "POST", body: JSON.stringify(body), signal: controller.signal }
            ))
            if (controller.signal.aborted) return
            validateReportMoney(result.items, effectiveColumns)
            setGeneratedQuery(body)

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
            if (controller.signal.aborted) return
            const msg = e instanceof Error ? e.message : "Не удалось сформировать отчёт. Проверьте настройки фильтров и попробуйте снова."
            setError(msg)
            setStatus("error")
        }

    }, [reportKey, filterValues, advancedFilterValues, filterFieldsMeta, syncToUrl, selectedFields, sortColumn, sortDirection, meta, availableFields, effectiveColumns, setGeneratedQuery, setIsDirty, setStatus, setError, setItems, setResultExtras, setServerDisplayRows])

    // ── Auto-generate on URL open ──────────────────────────────
    // If URL has filter params (f.*), auto-generate report on mount.
    // If we have cached items, we are returning to the tab, so skip.
    useEffect(() => {
        if (autoGenerated.current || metaLoading || !meta) return
        const hasUrlFilters = Array.from(searchParams.keys()).some((k) => k.startsWith("f."))
        if (hasUrlFilters) {
            autoGenerated.current = true
            // Defer to avoid synchronous setState inside effect body
            queueMicrotask(() => generate())
        }
    }, [metaLoading, meta, searchParams, generate])

    // ── Copy shareable link ────────────────────────────────────
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
    }, [setActiveGroupBy])

    // ── Sorting (client-side) ────────────────────────────────────────

    const handleSort = useCallback((key: string) => {
        setSelectionRange(null)
        setSortColumn((prev) => {
            if (prev === key) {
                queueMicrotask(() => setSortDirection((d) => (d === "asc" ? "desc" : "asc")))
                return key
            }
            queueMicrotask(() => setSortDirection("asc"))
            return key
        })
    }, [setSortColumn, setSortDirection])

    // ── Column visibility ────────────────────────────────────────────
    const setListColumns = useUserPrefsStore((s) => s.setListColumns)

    const toggleColumn = useCallback((key: string) => {
        setVisibleColumnKeys((prev) => {
            const next = prev.includes(key)
                ? prev.filter((k) => k !== key)
                : [...prev, key]
            queueMicrotask(() => setListColumns(`report:${reportKey}`, next))
            return next
        })
    }, [reportKey, setListColumns, setVisibleColumnKeys])

    const resetColumns = useCallback(() => {
        if (!meta) return
        const defaults = meta.columns.filter((c) => !c.defaultHidden).map((c) => c.key)
        setVisibleColumnKeys(defaults)
        setListColumns(`report:${reportKey}`, defaults)
    }, [meta, reportKey, setListColumns, setVisibleColumnKeys])

    const reorderColumn = useCallback((fromIndex: number, toIndex: number) => {
        setSelectionRange(null)
        setVisibleColumnKeys((prev) => {
            const next = [...prev]
            const [moved] = next.splice(fromIndex, 1)
            next.splice(toIndex, 0, moved)
            // Defer store persistence to avoid setState-during-render
            queueMicrotask(() => setListColumns(`report:${reportKey}`, next))
            return next
        })
    }, [reportKey, setListColumns, setVisibleColumnKeys])

    // ── Report Variants ────────────────────────────────────────
    const [variants, setVariants] = useTabState(tabKeys.variants, [])
    const [activeVariantId, setActiveVariantId] = useTabState(tabKeys.activeVariantId, null)

    // Load variants from backend on mount or reportKey change
    useEffect(() => {
        api.reports.variants.list(reportKey).then((data) => {
            setVariants(data)
            // If there's a default variant and no active variant is set, we could auto-load it
            // but for now let's just keep the list
        }).catch(console.error)
    }, [reportKey, setVariants])

    const saveVariant = useCallback(async (name: string, visibility: import("@/types/report-variant").VariantVisibility, isDefault: boolean) => {
        try {
            const config = {
                selectedFields,
                visibleColumns: visibleColumnKeys,
                groupBy: activeGroupBy,
                sortColumn,
                sortDirection,
                filters: { ...filterValues, __advancedState: advancedFilterValues },
                advancedFilters: buildFilterItems(advancedFilterValues, filterFieldsMeta),
            }

            const newVariant = await api.reports.variants.create({
                datasetKey: reportKey,
                name,
                visibility,
                isDefault,
                config,
            })

            setVariants((prev) => [...prev, newVariant])
            setActiveVariantId(newVariant.id)
            toast.success("Вариант отчета сохранен")
        } catch (e) {
            console.error(e)
            toast.error("Не удалось сохранить вариант. Проверьте соединение или попробуйте позже.")
        }
    }, [reportKey, selectedFields, visibleColumnKeys, activeGroupBy, sortColumn, sortDirection, filterValues, advancedFilterValues, filterFieldsMeta, setVariants, setActiveVariantId])

    const loadVariant = useCallback((id: string) => {
        const variant = variants.find((v) => v.id === id)
        if (!variant) return

        const { config, filters, advancedState } = restoreVariantState(variant)
        setSelectedFields(config.selectedFields)
        setVisibleColumnKeys(config.visibleColumns)
        setActiveGroupBy(config.groupBy)
        setSortColumn(config.sortColumn)
        setSortDirection(config.sortDirection)
        setFilterValues(filters)
        setAdvancedFilterValues(advancedState)

        setIsDirty(true) // Loading variant makes state dirty, needing generate()
        setActiveVariantId(id)
    }, [variants, setSelectedFields, setVisibleColumnKeys, setActiveGroupBy, setSortColumn, setSortDirection, setFilterValues, setAdvancedFilterValues, setIsDirty, setActiveVariantId])

    const deleteVariant = useCallback(async (id: string) => {
        try {
            await api.reports.variants.delete(id)
            setVariants((prev) => prev.filter((v) => v.id !== id))
            if (activeVariantId === id) {
                setActiveVariantId(null)
            }
            toast.success("Вариант отчета удален")
        } catch (e) {
            console.error(e)
            toast.error("Не удалось удалить вариант. Проверьте соединение или попробуйте позже.")
        }
    }, [activeVariantId, setVariants, setActiveVariantId])

    const updateVariant = useCallback(async (id: string, name: string, visibility: import("@/types/report-variant").VariantVisibility, isDefault: boolean) => {
        const existing = variants.find((v) => v.id === id)
        if (!existing) return

        try {
            const config = {
                selectedFields,
                visibleColumns: visibleColumnKeys,
                groupBy: activeGroupBy,
                sortColumn,
                sortDirection,
                filters: { ...filterValues, __advancedState: advancedFilterValues },
                advancedFilters: buildFilterItems(advancedFilterValues, filterFieldsMeta),
            }

            await api.reports.variants.update(id, {
                name,
                visibility,
                isDefault,
                config,
                version: existing.version,
            })

            // Refresh list from server to get updated version
            const refreshed = await api.reports.variants.list(reportKey)
            setVariants(refreshed)
            setActiveVariantId(id)
            toast.success("Вариант отчета обновлён")
        } catch (e) {
            console.error(e)
            toast.error("Не удалось обновить вариант. Проверьте соединение или попробуйте позже.")
        }
    }, [variants, reportKey, selectedFields, visibleColumnKeys, activeGroupBy, sortColumn, sortDirection, filterValues, advancedFilterValues, filterFieldsMeta, setVariants, setActiveVariantId])

    // ── Cell Selection ───────────────────────────────────────────────
    const [isDraggingSelection, setIsDraggingSelection] = useState(false)

    const onSelectionStart = useCallback((r: number, c: number) => {
        setSelectionRange({ start: { r, c }, end: { r, c } })
        setIsDraggingSelection(true)
    }, [])

    const onSelectionMove = useCallback((r: number, c: number) => {
        setIsDraggingSelection((dragging: boolean) => {
            if (dragging) {
                setSelectionRange((prev: { start: { r: number, c: number }, end: { r: number, c: number } } | null) => {
                    if (!prev) return null
                    return { start: prev.start, end: { r, c } }
                })
            }
            return dragging
        })
    }, [])

    const onSelectionEnd = useCallback(() => {
        setIsDraggingSelection(false)
    }, [])

    const resetSelection = useCallback(() => {
        setSelectionRange(null)
        setIsDraggingSelection(false)
    }, [])

    // ── View Mode ──────────────────────────────────────
    const [viewMode, setViewMode] = useTabState(tabKeys.viewMode, "table")

    // ── Server-grouped rows ─────────────────────────────────
    // Group and export the acknowledged query, even while the user edits filters.
    useEffect(() => {
        const controller = new AbortController()
        const fetchRows = async () => {
            if (items.length <= CLIENT_GROUPING_THRESHOLD || status !== "done" || !generatedQuery) {
                setServerDisplayRows(null)
                return
            }
            const params = new URLSearchParams()
            for (const g of activeGroupBy) {
                params.append("groupBy", g)
            }
            if (sortColumn) {
                params.set("sortBy", sortColumn.replace(/__/g, "."))
                params.set("sortDir", sortDirection)
            }

            const qs = params.toString()
            try {
                const result = groupedReportResponseSchema.parse(await apiFetch<JsonObject>(
                    `/reports/${reportKey}/grouped${qs ? `?${qs}` : ""}`,
                    { method: "POST", body: JSON.stringify(generatedQuery), signal: controller.signal },
                ))
                if (controller.signal.aborted) return
                validateReportMoney(result.rows.flatMap(row => row.kind === "data" ? [row.item] : []), effectiveColumns)
                setServerDisplayRows(result.rows)
            } catch {
                if (!controller.signal.aborted) {
                    setError("Не удалось сгруппировать отчёт. Сформируйте его повторно.")
                    setStatus("error")
                }
            }
        }
        void fetchRows()
        return () => controller.abort()
    }, [reportKey, generatedQuery, activeGroupBy, sortColumn, sortDirection, items.length, status, effectiveColumns, setServerDisplayRows, setError, setStatus])

    // ── Computed: sorted + grouped display rows (adaptive) ───────────
    const displayRows = useMemo(() => {
        if (items.length === 0) return []

        // Server-side grouping: use pre-built rows from /grouped
        if (items.length > CLIENT_GROUPING_THRESHOLD) {
            // Avoid rendering thousands of ungrouped DOM rows before the request
            // effect can run. A rejected server query must remain an error.
            return serverDisplayRows ?? []
        }

        // Client-side grouping (≤5000 rows)
        let sorted = items
        if (sortColumn) {
            sorted = sortItems(items, sortColumn, sortDirection)
        }

        const groupByDataKeys = activeGroupBy.map((groupKey) => {
            // First check if a dynamic column explicitly matches the path (e.g. merchant_id)
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
        exportReport: async () => {
            if (!generatedQuery) return
            const groupByDataKeys = activeGroupBy.map((groupKey) => {
                const dynamicCol = effectiveColumns.find((c) => c.key === groupKey)
                if (dynamicCol && dynamicCol.key) return dynamicCol.key
                const baseCol = meta?.columns.find((c) => c.key === groupKey || c.key === `${groupKey}__name`)
                return baseCol ? baseCol.key : groupKey
            })

            const body = DatasetQueryRequestSchema.parse({
                ...generatedQuery,
                limit: undefined,
                offset: undefined,
                orderBy: sortColumn ? sortColumn.replace(/__/g, ".") : undefined,
                orderDir: sortDirection,
                exportColumns: visibleColumnKeys,
                exportGroupBy: groupByDataKeys,
            })
            const exportPromise = apiFetchBlob(
                `/reports/${reportKey}/export`,
                { method: "POST", body: JSON.stringify(body) }
            ).then(({ blob, filename }) => {
                const url = URL.createObjectURL(blob)
                const a = document.createElement("a")
                a.href = url
                a.download = filename
                document.body.appendChild(a)
                a.click()
                document.body.removeChild(a)
                URL.revokeObjectURL(url)
            })
            toast.promise(exportPromise, {
                loading: "Экспорт отчёта…",
                success: "Файл скачан",
                error: (e) => e instanceof Error ? e.message : "Ошибка экспорта",
            })
        },
        activeGroupBy,
        toggleGroupBy,
        sortColumn,
        sortDirection,
        handleSort,
        visibleColumnKeys,
        toggleColumn,
        resetColumns,
        reorderColumn,
        resultExtras,
        //
        variants,
        activeVariantId,
        saveVariant,
        updateVariant,
        loadVariant,
        deleteVariant,
        selectionRange,
        isDraggingSelection,
        onSelectionStart,
        onSelectionMove,
        onSelectionEnd,
        resetSelection,
        copyShareLink,
        viewMode,
        setViewMode,
        // Query Engine
        availableFields,
        selectedFields,
        toggleField,
        effectiveColumns,
        // Advanced Filters (FilterSidebar)
        filterFieldsMeta,
        advancedFilterValues,
        onAdvancedFilterValuesChange,
        isDirty,
    }
}

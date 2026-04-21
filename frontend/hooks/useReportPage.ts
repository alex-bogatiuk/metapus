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
    ReportStatus,
    DisplayRow,
} from "@/types/report-meta"

// ── Types ───────────────────────────────────────────────────────────────

/** Filter values are stored as key → primitive/array. */
export type ReportFilterValues = Record<string, unknown>

/** Generic report result with items array. */
interface ReportResult {
    items: Record<string, unknown>[]
    [key: string]: unknown
}

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
}

// ── Hook ────────────────────────────────────────────────────────────────

export function useReportPage(reportKey: string): UseReportPageReturn {
    const router = useRouter()
    const pathname = usePathname()
    const searchParams = useSearchParams()

    // ── Metadata ─────────────────────────────────────────────────────
    const [meta, setMeta] = useState<ReportMeta | null>(null)
    const [metaLoading, setMetaLoading] = useState(true)

    useEffect(() => {
        let cancelled = false
        setMetaLoading(true)
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
    }, [])

    const resetFilters = useCallback(() => {
        setFilterValues({})
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
        setStatus("loading")
        setError(null)
        syncToUrl(filterValues)

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
            const qs = params.toString()
            const result = await apiFetch<ReportResult>(
                `/reports/${reportKey}${qs ? `?${qs}` : ""}`
            )

            const reportItems = result.items ?? []
            setItems(reportItems)

            const { items: _, ...extras } = result
            setResultExtras(extras)

            setStatus(reportItems.length > 0 ? "done" : "empty")
        } catch (e) {
            const msg = e instanceof Error ? e.message : "Ошибка формирования отчёта"
            setError(msg)
            setStatus("error")
        }
    }, [reportKey, filterValues, syncToUrl])

    // ── Auto-generate on URL open [#15] ──────────────────────────────
    // If URL has filter params (f.*), auto-generate report on mount.
    const autoGenerated = useRef(false)
    useEffect(() => {
        if (autoGenerated.current || metaLoading || !meta) return
        const hasUrlFilters = Array.from(searchParams.keys()).some((k) => k.startsWith("f."))
        if (hasUrlFilters) {
            autoGenerated.current = true
            generate()
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
    const [activeGroupBy, setActiveGroupBy] = useState<string[]>([])

    const toggleGroupBy = useCallback((key: string) => {
        setActiveGroupBy((prev) =>
            prev.includes(key)
                ? prev.filter((k) => k !== key)
                : [...prev, key]
        )
    }, [])

    // ── Sorting (client-side) ────────────────────────────────────────
    const [sortColumn, setSortColumn] = useState<string | null>(null)
    const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc")

    const sortInitialized = useRef(false)
    useEffect(() => {
        if (meta?.defaultSort && !sortInitialized.current) {
            sortInitialized.current = true
            setSortColumn(meta.defaultSort.column)
            setSortDirection(meta.defaultSort.direction)
        }
    }, [meta])

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
    const [visibleColumnKeys, setVisibleColumnKeys] = useState<string[]>([])
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

    // ── Computed: sorted + grouped display rows ──────────────────────
    const displayRows = useMemo(() => {
        if (items.length === 0) return []

        let sorted = items
        if (sortColumn) {
            sorted = sortItems(items, sortColumn, sortDirection)
        }

        return buildDisplayRows(sorted, activeGroupBy, meta?.totals ?? [])
    }, [items, sortColumn, sortDirection, activeGroupBy, meta?.totals])

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
    }
}

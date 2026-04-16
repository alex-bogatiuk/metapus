"use client"

/**
 * ReferencePickerDialog — metadata-driven modal for browsing and selecting
 * catalog/document references.
 *
 * Analogous to:
 *   - 1C: "Показать все" / кнопка "..." в поле ввода
 *   - SAP Fiori: ValueHelp (F4) dialog
 *   - Odoo: "Search More..." in Many2one fields
 *
 * Fully automatic: no configuration needed per entity. Columns, title, and
 * search are driven by backend metadata (`GET /meta/:entityName`).
 *
 * V1 scope: single-select, search + sort + table, infinite scroll,
 * keyboard navigation (Arrow Up/Down, Enter). No FilterSidebar.
 */

import { useState, useEffect, useCallback, useRef, useMemo } from "react"
import { Loader2, ArrowUp, ArrowDown, ArrowUpDown, Search } from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import { apiFetch } from "@/lib/api"
import { useCompactMode } from "@/hooks/useCompactMode"
import { resolveEntityFromEndpoint } from "@/lib/reference-utils"
import {
    buildColumnsFromFields,
    formatCellValue,
    type MetadataField,
    type AutoColumn,
} from "@/lib/metadata-columns"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { useColumnResize, type ColumnResizeDef } from "@/hooks/useColumnResize"
import type { CursorListResponse } from "@/types/common"

// ── Props ───────────────────────────────────────────────────────────────

interface ReferencePickerDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
    /** API endpoint path, e.g. "/catalog/counterparties" */
    apiEndpoint: string
    /** Single-select callback: returns selected id + display name */
    onSelect: (id: string, name: string) => void
}

// ── Module-level metadata cache ─────────────────────────────────────────
// Caches resolved columns per entityName to avoid repeated /meta/ API calls.
// Analogous to _refNameCache in reference-field.tsx.
const _metaColumnsCache = new Map<string, AutoColumn[]>()

const DEFAULT_LIMIT = 50

// ── Column width persistence (localStorage per entityName) ──────────────
const COL_WIDTHS_PREFIX = "metapus-picker-colwidths:"

function getStoredColWidths(entityName: string): Record<string, number> | undefined {
    if (typeof window === "undefined" || !entityName) return undefined
    try {
        const raw = localStorage.getItem(COL_WIDTHS_PREFIX + entityName)
        return raw ? JSON.parse(raw) as Record<string, number> : undefined
    } catch {
        return undefined
    }
}

function saveColWidths(entityName: string, widths: Record<string, number>): void {
    if (typeof window === "undefined" || !entityName) return
    try {
        localStorage.setItem(COL_WIDTHS_PREFIX + entityName, JSON.stringify(widths))
    } catch {
        // ignore quota errors
    }
}

// ── Row data type ───────────────────────────────────────────────────────

type RowData = Record<string, unknown> & { id: string }

// ── Component ───────────────────────────────────────────────────────────

export function ReferencePickerDialog({
    open,
    onOpenChange,
    apiEndpoint,
    onSelect,
}: ReferencePickerDialogProps) {
    // ── Metadata resolution ─────────────────────────────────────────────
    const resolved = useMemo(() => resolveEntityFromEndpoint(apiEndpoint), [apiEndpoint])
    const entityName = resolved?.entityName ?? ""
    const title = resolved?.entity.presentation.plural ?? "Выбор"

    // ── Columns from metadata ───────────────────────────────────────────
    const [columns, setColumns] = useState<AutoColumn[]>(() =>
        _metaColumnsCache.get(entityName) ?? []
    )
    const [columnsLoading, setColumnsLoading] = useState(false)

    // Load metadata columns (cached)
    useEffect(() => {
        if (!open || !entityName) return

        const cached = _metaColumnsCache.get(entityName)
        if (cached) {
            setColumns(cached)
            return
        }

        let cancelled = false
        setColumnsLoading(true)

        apiFetch<{ fields: MetadataField[] }>(`/meta/${entityName}`)
            .then((meta) => {
                if (cancelled) return
                const cols = buildColumnsFromFields(meta.fields, 6)
                _metaColumnsCache.set(entityName, cols)
                setColumns(cols)
            })
            .catch(() => {
                if (cancelled) return
                // Fallback: code + name
                const fallback: AutoColumn[] = [
                    { key: "code", label: "Код", type: "string" },
                    { key: "name", label: "Наименование", type: "string" },
                ]
                setColumns(fallback)
            })
            .finally(() => {
                if (!cancelled) setColumnsLoading(false)
            })

        return () => { cancelled = true }
    }, [open, entityName])

    // ── Column resize (drag handles + localStorage persistence) ─────────
    const resizeColumns = useMemo<ColumnResizeDef[]>(
        () => columns.map((col) => ({
            key: col.key,
            width: col.type === "boolean" ? 80 : 150,
            minWidth: 60,
        })),
        [columns],
    )

    const storedColWidths = useMemo(
        () => getStoredColWidths(entityName),
        [entityName],
    )

    const handleWidthsChange = useCallback(
        (widths: Record<string, number>) => {
            saveColWidths(entityName, widths)
        },
        [entityName],
    )

    const { colWidths, onResizeStart, isResizing } = useColumnResize({
        columns: resizeColumns,
        storedWidths: storedColWidths,
        onWidthsChange: handleWidthsChange,
    })

    // ── Data loading state (infinite scroll — items are appended) ───────
    const [items, setItems] = useState<RowData[]>([])
    const [loading, setLoading] = useState(false)
    const [loadingMore, setLoadingMore] = useState(false)
    const [totalCount, setTotalCount] = useState(0)
    const [hasMore, setHasMore] = useState(false)
    const [nextCursor, setNextCursor] = useState<string | null>(null)

    // ── Search ──────────────────────────────────────────────────────────
    const [search, setSearch] = useState("")
    const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

    // ── Sort (local state, not URL-based — this is a dialog) ────────────
    const [sortField, setSortField] = useState("name")
    const [sortDir, setSortDir] = useState<"asc" | "desc">("asc")

    // ── Selection ───────────────────────────────────────────────────────
    const [focusedId, setFocusedId] = useState<string | null>(null)

    // ── Scroll container ref for ScrollSentinel ─────────────────────────
    const scrollContainerRef = useRef<HTMLDivElement>(null)

    // ── Fetch data (initial load or full reset) ─────────────────────────
    const fetchData = useCallback(
        async () => {
            if (!apiEndpoint) return
            setLoading(true)
            try {
                const params = new URLSearchParams()
                params.set("limit", String(DEFAULT_LIMIT))
                if (search.trim()) params.set("search", search.trim())
                const orderBy = sortDir === "desc" ? `-${sortField}` : sortField
                params.set("orderBy", orderBy)

                const result = await apiFetch<CursorListResponse<RowData>>(
                    `${apiEndpoint}?${params.toString()}`
                )
                setItems(result.items ?? [])
                setTotalCount(result.totalCount ?? 0)
                setHasMore(result.hasMore)
                setNextCursor(result.nextCursor ?? null)

                // Auto-focus first item
                if (result.items?.length) {
                    setFocusedId(result.items[0].id)
                } else {
                    setFocusedId(null)
                }
            } catch {
                setItems([])
                setFocusedId(null)
            } finally {
                setLoading(false)
            }
        },
        [apiEndpoint, search, sortField, sortDir],
    )

    // ── Load more (infinite scroll — append to existing items) ───────────
    const fetchMore = useCallback(async () => {
        if (!apiEndpoint || !nextCursor || loadingMore) return
        setLoadingMore(true)
        try {
            const params = new URLSearchParams()
            params.set("limit", String(DEFAULT_LIMIT))
            if (search.trim()) params.set("search", search.trim())
            const orderBy = sortDir === "desc" ? `-${sortField}` : sortField
            params.set("orderBy", orderBy)
            params.set("after", nextCursor)

            const result = await apiFetch<CursorListResponse<RowData>>(
                `${apiEndpoint}?${params.toString()}`
            )
            setItems((prev) => [...prev, ...(result.items ?? [])])
            setHasMore(result.hasMore)
            setNextCursor(result.nextCursor ?? null)
        } catch {
            // silently fail — user can scroll up
        } finally {
            setLoadingMore(false)
        }
    }, [apiEndpoint, nextCursor, loadingMore, search, sortField, sortDir])

    // ── Initial load & debounced search ─────────────────────────────────
    const initialLoadRef = useRef(false)

    useEffect(() => {
        if (!open) {
            // Reset state when dialog closes
            initialLoadRef.current = false
            setSearch("")
            setSortField("name")
            setSortDir("asc")
            setItems([])
            setFocusedId(null)
            setNextCursor(null)
            setHasMore(false)
            return
        }

        if (!initialLoadRef.current) {
            initialLoadRef.current = true
            fetchData()
            return
        }

        // Debounce subsequent search changes
        if (debounceRef.current) clearTimeout(debounceRef.current)
        debounceRef.current = setTimeout(() => fetchData(), 250)

        return () => {
            if (debounceRef.current) clearTimeout(debounceRef.current)
        }
    }, [open, fetchData])

    // ── Sort handler (resets to first page) ─────────────────────────────
    const handleSort = (key: string) => {
        if (sortField === key) {
            setSortDir((d) => (d === "asc" ? "desc" : "asc"))
        } else {
            setSortField(key)
            setSortDir("asc")
        }
    }

    // ── Selection handlers ──────────────────────────────────────────────
    const getDisplayName = (item: RowData): string => {
        return String(
            item.name ?? item.number ?? item.code ?? `${String(item.id).slice(0, 8)}…`
        )
    }

    const handleSelectItem = useCallback((item: RowData) => {
        onSelect(item.id, getDisplayName(item))
    }, [onSelect])

    const handleSelectFocused = useCallback(() => {
        if (!focusedId) return
        const item = items.find((i) => i.id === focusedId)
        if (item) handleSelectItem(item)
    }, [focusedId, items, handleSelectItem])

    // ── Keyboard navigation ─────────────────────────────────────────────
    const tableContainerRef = useRef<HTMLDivElement>(null)

    const handleKeyDown = useCallback(
        (e: React.KeyboardEvent) => {
            if (!items.length) return

            const currentIndex = focusedId
                ? items.findIndex((i) => i.id === focusedId)
                : -1

            switch (e.key) {
                case "ArrowDown": {
                    e.preventDefault()
                    const nextIdx = Math.min(currentIndex + 1, items.length - 1)
                    setFocusedId(items[nextIdx].id)
                    // Scroll row into view
                    const row = tableContainerRef.current?.querySelector(
                        `[data-row-id="${items[nextIdx].id}"]`
                    )
                    row?.scrollIntoView({ block: "nearest" })
                    break
                }
                case "ArrowUp": {
                    e.preventDefault()
                    const prevIdx = Math.max(currentIndex - 1, 0)
                    setFocusedId(items[prevIdx].id)
                    const row = tableContainerRef.current?.querySelector(
                        `[data-row-id="${items[prevIdx].id}"]`
                    )
                    row?.scrollIntoView({ block: "nearest" })
                    break
                }
                case "Enter": {
                    e.preventDefault()
                    handleSelectFocused()
                    break
                }
            }
        },
        [items, focusedId, handleSelectFocused],
    )

    // ── Sort icon component ─────────────────────────────────────────────
    const SortIcon = ({ colKey }: { colKey: string }) => {
        if (sortField !== colKey) {
            return <ArrowUpDown className="ml-1 inline h-3 w-3 shrink-0 opacity-0 group-hover:opacity-40" />
        }
        return sortDir === "asc" ? (
            <ArrowUp className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
        ) : (
            <ArrowDown className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
        )
    }

    // ── Render ───────────────────────────────────────────────────────────

    const showInitialLoading = loading && items.length === 0

    const compact = useCompactMode()
    const rowH = compact ? "h-7" : "h-9"
    const cellPx = compact ? "px-2" : "px-4"

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                className="max-w-5xl w-[90vw] flex flex-col gap-0 p-0"
                onOpenAutoFocus={(e) => e.preventDefault()}
            >
                <DialogHeader className="px-6 pt-6 pb-3">
                    <DialogTitle>{title}</DialogTitle>
                </DialogHeader>

                {/* Search */}
                <div className="px-6 pb-3">
                    <div className="relative">
                        <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            placeholder="Поиск…"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            className="pl-8 h-9"
                            autoFocus
                        />
                    </div>
                </div>

                {/* Table with infinite scroll */}
                <div
                    ref={tableContainerRef}
                    className="flex-1 min-h-0 px-6"
                    onKeyDown={handleKeyDown}
                    tabIndex={0}
                >
                    <ScrollArea
                        viewportRef={scrollContainerRef}
                        className="h-[55vh] rounded-md border"
                    >
                        <table
                            className={cn(
                                "w-full text-sm table-fixed",
                                isResizing && "select-none",
                            )}
                        >
                            <colgroup>
                                {columns.map((col, i) => (
                                    <col
                                        key={col.key}
                                        style={{ width: colWidths[i] ?? undefined }}
                                    />
                                ))}
                            </colgroup>
                            <thead className="sticky top-0 z-10">
                                <tr className="border-b bg-muted/70">
                                    {columns.map((col, colIndex) => (
                                        <th
                                            key={col.key}
                                            className={cn(
                                                "relative text-xs font-medium text-muted-foreground select-none cursor-pointer group hover:text-foreground transition-colors",
                                                cellPx, compact ? "py-1" : "py-2",
                                                col.align === "right" ? "text-right" : "text-left",
                                            )}
                                            onClick={() => { if (!isResizing) handleSort(col.key) }}
                                        >
                                            <div className="truncate">
                                                {col.label}
                                                <SortIcon colKey={col.key} />
                                            </div>

                                            {/* Resize handle */}
                                            <div
                                                className="absolute right-0 top-0 h-full w-[5px] cursor-col-resize z-20 group/resize hover:bg-primary/30 active:bg-primary/50"
                                                onMouseDown={(e) => onResizeStart(colIndex, e)}
                                                onClick={(e) => e.stopPropagation()}
                                            >
                                                <div className="absolute right-0 top-1/4 h-1/2 w-[1px] bg-border group-hover/resize:bg-primary/60" />
                                            </div>
                                        </th>
                                    ))}
                                </tr>
                            </thead>
                            <tbody>
                                {(showInitialLoading || columnsLoading) ? (
                                    <tr>
                                        <td colSpan={columns.length} className="py-12 text-center">
                                            <Loader2 className="inline-block h-5 w-5 animate-spin text-muted-foreground" />
                                        </td>
                                    </tr>
                                ) : items.length === 0 ? (
                                    <tr>
                                        <td
                                            colSpan={columns.length}
                                            className="py-12 text-center text-sm text-muted-foreground"
                                        >
                                            Ничего не найдено
                                        </td>
                                    </tr>
                                ) : (
                                    items.map((item) => {
                                        const isFocused = focusedId === item.id
                                        return (
                                            <tr
                                                key={item.id}
                                                data-row-id={item.id}
                                                className={cn(
                                                    "border-b transition-colors cursor-pointer",
                                                    rowH,
                                                    isFocused
                                                        ? "bg-primary/15 ring-1 ring-inset ring-primary/30"
                                                        : "hover:bg-primary/5",
                                                )}
                                                onClick={() => setFocusedId(item.id)}
                                                onDoubleClick={() => handleSelectItem(item)}
                                            >
                                                {columns.map((col) => (
                                                    <td
                                                        key={col.key}
                                                        className={cn(
                                                            "py-0",
                                                            cellPx, rowH,
                                                            col.align === "right" ? "text-right" : "text-left",
                                                        )}
                                                    >
                                                        <div className={cn("flex items-center min-w-0 whitespace-nowrap overflow-hidden text-ellipsis", rowH)}>
                                                            {formatCellValue(item[col.key], col.type)}
                                                        </div>
                                                    </td>
                                                ))}
                                            </tr>
                                        )
                                    })
                                )}
                            </tbody>
                        </table>

                        {/* Infinite scroll sentinel */}
                        <ScrollSentinel
                            onIntersect={fetchMore}
                            loading={loadingMore}
                            enabled={hasMore && !loading}
                            scrollContainer={scrollContainerRef}
                            rootMargin="100px"
                        />
                        <ScrollBar orientation="horizontal" />
                    </ScrollArea>
                </div>

                {/* Footer */}
                <DialogFooter className="px-6 py-3 border-t flex-row items-center justify-between sm:justify-between">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <span>Показано: {items.length} из {totalCount}</span>
                    </div>
                    <div className="flex gap-2">
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onOpenChange(false)}
                        >
                            Отмена
                        </Button>
                        <Button
                            size="sm"
                            disabled={!focusedId}
                            onClick={handleSelectFocused}
                        >
                            Выбрать
                        </Button>
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}

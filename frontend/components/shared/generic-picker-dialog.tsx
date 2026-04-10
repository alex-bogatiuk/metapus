"use client"

/**
 * GenericPickerDialog — universal, metadata-driven picker dialog for any entity.
 *
 * Analogous to:
 *   - 1С: "Форма подбора" (generic, auto-resolves columns from metadata)
 *   - SAP Fiori: ValueHelp Multi-Select dialog
 *
 * Used as the default picker when no specialized picker exists.
 * Features:
 *   - Metadata-driven columns (from GET /meta/:entityName)
 *   - Search with debounce
 *   - Sortable columns with resize handles
 *   - Infinite scroll (ScrollSentinel)
 *   - Keyboard navigation (ArrowUp/Down, Enter)
 *   - Single-select or multi-select mode
 *
 * Pattern #2: Configuration over Boilerplate.
 * Pattern #6: Composition — reuses usePickerDialog, useColumnResize, ScrollSentinel.
 */

import { useState, useEffect, useMemo, useCallback } from "react"
import { Loader2, ArrowUp, ArrowDown, ArrowUpDown, Search, Check } from "lucide-react"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import { apiFetch } from "@/lib/api"
import { resolveEntityFromEndpoint } from "@/lib/reference-utils"
import {
    buildColumnsFromFields,
    formatCellValue,
    type MetadataField,
    type AutoColumn,
} from "@/lib/metadata-columns"
import { ScrollSentinel } from "@/components/shared/scroll-sentinel"
import { useColumnResize, type ColumnResizeDef } from "@/hooks/useColumnResize"
import { usePickerDialog } from "@/hooks/usePickerDialog"
import type { GenericPickerDialogProps, PickedItem } from "@/types/picker"

// ── Module-level metadata cache ─────────────────────────────────────────
const _metaColumnsCache = new Map<string, AutoColumn[]>()

// ── Column width persistence ─────────────────────────────────────────────
const COL_WIDTHS_PREFIX = "metapus-gpicker-colwidths:"

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
    } catch { /* ignore */ }
}

// ── Component ───────────────────────────────────────────────────────────

export function GenericPickerDialog({
    open,
    onOpenChange,
    apiEndpoint,
    onPick,
    multiSelect = true,
    title: titleOverride,
}: GenericPickerDialogProps) {
    // ── Entity resolution ───────────────────────────────────────────────
    const resolved = useMemo(() => resolveEntityFromEndpoint(apiEndpoint), [apiEndpoint])
    const entityName = resolved?.entityName ?? ""
    const autoTitle = resolved?.entity.presentation.plural ?? "Подбор"
    const displayTitle = titleOverride ?? autoTitle

    // ── Data via shared hook ────────────────────────────────────────────
    const {
        items: pickerItems,
        loading: pickerLoading,
        loadingMore: pickerLoadingMore,
        totalCount: pickerTotalCount,
        hasMore: pickerHasMore,
        search: pickerSearch,
        setSearch: pickerSetSearch,
        sortField: pickerSortField,
        sortDir: pickerSortDir,
        handleSort: pickerHandleSort,
        focusedId: pickerFocusedId,
        setFocusedId: pickerSetFocusedId,
        quantities: pickerQuantities,
        setQuantity: pickerSetQuantity,
        pickedCount: pickerPickedCount,
        fetchMore: pickerFetchMore,
        handleKeyDown: pickerHandleKeyDown,
        scrollContainerRef: pickerScrollContainerRef,
        tableContainerRef: pickerTableContainerRef,
    } = usePickerDialog({ apiEndpoint, open })

    // ── Metadata columns ────────────────────────────────────────────────
    // Use cache-derived initial value to avoid synchronous setState in effect
    const cachedColumns = useMemo(
        () => _metaColumnsCache.get(entityName) ?? null,
        [entityName],
    )
    const [fetchedColumns, setFetchedColumns] = useState<AutoColumn[] | null>(null)
    // columnsLoading starts true if no cache — effect will set to false after fetch
    const [columnsLoading, setColumnsLoading] = useState(!cachedColumns)

    const columns = useMemo(
        () => fetchedColumns ?? cachedColumns ?? [],
        [fetchedColumns, cachedColumns],
    )

    useEffect(() => {
        if (!open || !entityName) return
        if (_metaColumnsCache.has(entityName)) return

        let cancelled = false

        apiFetch<{ fields: MetadataField[] }>(`/meta/${entityName}`)
            .then((meta) => {
                if (cancelled) return
                const cols = buildColumnsFromFields(meta.fields, 6)
                _metaColumnsCache.set(entityName, cols)
                setFetchedColumns(cols)
            })
            .catch(() => {
                if (cancelled) return
                const fallback: AutoColumn[] = [
                    { key: "code", label: "Код", type: "string" },
                    { key: "name", label: "Наименование", type: "string" },
                ]
                setFetchedColumns(fallback)
            })
            .finally(() => {
                if (!cancelled) setColumnsLoading(false)
            })

        return () => { cancelled = true }
    }, [open, entityName])

    // ── Column resize ───────────────────────────────────────────────────
    const resizeColumns = useMemo<ColumnResizeDef[]>(
        () => columns.map((col) => ({
            key: col.key,
            width: col.type === "boolean" ? 80 : 150,
            minWidth: 60,
        })),
        [columns],
    )

    const storedColWidths = useMemo(() => getStoredColWidths(entityName), [entityName])

    const handleWidthsChange = useCallback(
        (widths: Record<string, number>) => saveColWidths(entityName, widths),
        [entityName],
    )

    const { colWidths, onResizeStart, isResizing } = useColumnResize({
        columns: resizeColumns,
        storedWidths: storedColWidths,
        onWidthsChange: handleWidthsChange,
    })

    // ── Selection logic ─────────────────────────────────────────────────
    const getDisplayName = (item: Record<string, unknown> & { id: string }): string =>
        String(item.name ?? item.number ?? item.code ?? `${String(item.id).slice(0, 8)}…`)

    const handleToggleItem = (item: Record<string, unknown> & { id: string }) => {
        if (multiSelect) {
            const current = pickerQuantities.get(item.id) || 0
            pickerSetQuantity(item.id, current > 0 ? 0 : 1)
        } else {
            // Single-select: immediately pick
            onPick([{ id: item.id, name: getDisplayName(item), quantity: 1 }])
        }
    }

    const handleConfirm = () => {
        const picked: PickedItem[] = []
        for (const [id, qty] of pickerQuantities.entries()) {
            if (qty <= 0) continue
            const item = pickerItems.find((i) => i.id === id)
            if (item) {
                picked.push({
                    id: item.id,
                    name: getDisplayName(item),
                    code: item.code != null ? String(item.code) : undefined,
                    quantity: qty,
                })
            }
        }
        onPick(picked)
    }

    // ── Keyboard: Enter = toggle in multi-select, pick in single-select ─
    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === "Enter") {
            e.preventDefault()
            if (pickerFocusedId) {
                const item = pickerItems.find((i) => i.id === pickerFocusedId)
                if (item) handleToggleItem(item)
            }
            return
        }
        pickerHandleKeyDown(e)
    }

    // ── Sort icon ───────────────────────────────────────────────────────
    const SortIcon = ({ colKey }: { colKey: string }) => {
        if (pickerSortField !== colKey) {
            return <ArrowUpDown className="ml-1 inline h-3 w-3 shrink-0 opacity-0 group-hover:opacity-40" />
        }
        return pickerSortDir === "asc" ? (
            <ArrowUp className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
        ) : (
            <ArrowDown className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
        )
    }

    // ── Render ───────────────────────────────────────────────────────────
    const showInitialLoading = pickerLoading && pickerItems.length === 0

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                className="max-w-5xl w-[90vw] flex flex-col gap-0 p-0"
                onOpenAutoFocus={(e) => e.preventDefault()}
            >
                <DialogHeader className="px-6 pt-6 pb-3">
                    <DialogTitle>{displayTitle}</DialogTitle>
                </DialogHeader>

                {/* Search */}
                <div className="px-6 pb-3">
                    <div className="relative">
                        <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            placeholder="Поиск…"
                            value={pickerSearch}
                            onChange={(e) => pickerSetSearch(e.target.value)}
                            className="pl-8 h-9"
                            autoFocus
                        />
                    </div>
                </div>

                {/* Table */}
                <div
                    ref={pickerTableContainerRef}
                    className="flex-1 min-h-0 px-6"
                    onKeyDown={handleKeyDown}
                    tabIndex={0}
                >
                    <ScrollArea
                        viewportRef={pickerScrollContainerRef}
                        className="h-[55vh] rounded-md border"
                    >
                        <table
                            className={cn(
                                "w-full text-sm table-fixed",
                                isResizing && "select-none",
                            )}
                        >
                            <colgroup>
                                {multiSelect && <col style={{ width: 36 }} />}
                                {columns.map((col, i) => (
                                    <col key={col.key} style={{ width: colWidths[i] ?? undefined }} />
                                ))}
                            </colgroup>
                            <thead className="sticky top-0 z-10">
                                <tr className="border-b bg-muted/70">
                                    {multiSelect && (
                                        <th className="px-2 py-2 text-xs font-medium text-muted-foreground w-[36px]" />
                                    )}
                                    {columns.map((col, colIndex) => (
                                        <th
                                            key={col.key}
                                            className={cn(
                                                "relative px-4 py-2 text-xs font-medium text-muted-foreground select-none cursor-pointer group hover:text-foreground transition-colors",
                                                col.align === "right" ? "text-right" : "text-left",
                                            )}
                                            onClick={() => { if (!isResizing) pickerHandleSort(col.key) }}
                                        >
                                            <div className="truncate">
                                                {col.label}
                                                <SortIcon colKey={col.key} />
                                            </div>
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
                                        <td colSpan={columns.length + (multiSelect ? 1 : 0)} className="py-12 text-center">
                                            <Loader2 className="inline-block h-5 w-5 animate-spin text-muted-foreground" />
                                        </td>
                                    </tr>
                                ) : pickerItems.length === 0 ? (
                                    <tr>
                                        <td
                                            colSpan={columns.length + (multiSelect ? 1 : 0)}
                                            className="py-12 text-center text-sm text-muted-foreground"
                                        >
                                            Ничего не найдено
                                        </td>
                                    </tr>
                                ) : (
                                    pickerItems.map((item) => {
                                        const isFocused = pickerFocusedId === item.id
                                        const isSelected = (pickerQuantities.get(item.id) || 0) > 0
                                        return (
                                            <tr
                                                key={item.id}
                                                data-row-id={item.id}
                                                className={cn(
                                                    "border-b transition-colors cursor-pointer h-9",
                                                    isFocused
                                                        ? "bg-primary/15 ring-1 ring-inset ring-primary/30"
                                                        : isSelected
                                                            ? "bg-emerald-50/50 dark:bg-emerald-950/20"
                                                            : "hover:bg-primary/5",
                                                )}
                                                onClick={() => pickerSetFocusedId(item.id)}
                                                onDoubleClick={() => handleToggleItem(item)}
                                            >
                                                {multiSelect && (
                                                    <td className="px-2 py-0 h-9 text-center">
                                                        {isSelected && <Check className="h-3.5 w-3.5 text-emerald-600 inline-block" />}
                                                    </td>
                                                )}
                                                {columns.map((col) => (
                                                    <td
                                                        key={col.key}
                                                        className={cn(
                                                            "px-4 py-0 h-9 max-h-9",
                                                            col.align === "right" ? "text-right" : "text-left",
                                                        )}
                                                    >
                                                        <div className="flex items-center h-9 min-w-0 whitespace-nowrap overflow-hidden text-ellipsis">
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

                        <ScrollSentinel
                            onIntersect={pickerFetchMore}
                            loading={pickerLoadingMore}
                            enabled={pickerHasMore && !pickerLoading}
                            scrollContainer={pickerScrollContainerRef}
                            rootMargin="100px"
                        />
                        <ScrollBar orientation="horizontal" />
                    </ScrollArea>
                </div>

                {/* Footer */}
                <DialogFooter className="px-6 py-3 border-t flex-row items-center justify-between sm:justify-between">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        <span>Показано: {pickerItems.length} из {pickerTotalCount}</span>
                        {multiSelect && pickerPickedCount > 0 && (
                            <Badge variant="secondary" className="text-[10px]">
                                Выбрано: {pickerPickedCount}
                            </Badge>
                        )}
                    </div>
                    <div className="flex gap-2">
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={() => onOpenChange(false)}
                        >
                            Отмена
                        </Button>
                        {multiSelect ? (
                            <Button
                                size="sm"
                                disabled={pickerPickedCount === 0}
                                onClick={handleConfirm}
                            >
                                Добавить ({pickerPickedCount})
                            </Button>
                        ) : (
                            <Button
                                size="sm"
                                disabled={!pickerFocusedId}
                                onClick={() => {
                                    if (pickerFocusedId) {
                                        const item = pickerItems.find((i) => i.id === pickerFocusedId)
                                        if (item) handleToggleItem(item)
                                    }
                                }}
                            >
                                Выбрать
                            </Button>
                        )}
                    </div>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}

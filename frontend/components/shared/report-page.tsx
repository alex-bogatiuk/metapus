"use client"

/**
 * ReportPage — generic container for all metadata-driven reports.
 *
 * Wave 1: Core layout (toolbar, filter controls, data table)
 * Wave 3: Drill-down panel, report variants, URL sharing, view toggle
 *
 * No report-specific code — driven entirely by useReportPage hook.
 */

import React, { useCallback, useEffect, useMemo, useState } from "react"
import { format } from "date-fns"
import { useRouter } from "next/navigation"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import {
    DropdownMenu,
    DropdownMenuTrigger,
    DropdownMenuContent,
    DropdownMenuCheckboxItem,
    DropdownMenuSeparator,
    DropdownMenuItem,
    DropdownMenuLabel,
} from "@/components/ui/dropdown-menu"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogFooter,
} from "@/components/ui/dialog"
import {
    Sheet,
    SheetContent,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import {
    Loader2, Play, Download, Columns3, Layers, AlertCircle, Inbox,
    ChevronDown, ChevronRight, Link2, Save, BookmarkCheck, Trash2, Table2, BarChart3, X,
    ListTree,
} from "lucide-react"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { DatePicker } from "@/components/ui/date-picker"
import type { UseReportPageReturn } from "@/hooks/useReportPage"
import type { DisplayRow, ReportColumnDef, FieldTreeNode } from "@/types/report-meta"
import { FilterSidebar } from "@/components/shared/filter-sidebar"
import {
    DndContext,
    closestCenter,
    KeyboardSensor,
    PointerSensor,
    useSensor,
    useSensors,
    type DragEndEvent,
} from "@dnd-kit/core"
import {
    SortableContext,
    horizontalListSortingStrategy,
    useSortable,
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import Decimal from "decimal.js"

// ── Props ───────────────────────────────────────────────────────────────

interface ReportPageProps {
    report: UseReportPageReturn
    /** Optional custom filter panel. Falls back to auto-generated from metadata. */
    filterSlot?: React.ReactNode
    /** Optional header content (breadcrumbs, etc.) */
    headerSlot?: React.ReactNode
}

// ── Component ───────────────────────────────────────────────────────────

export function ReportPage({ report, filterSlot, headerSlot }: ReportPageProps) {
    const { meta, metaLoading, status, generate } = report

    if (metaLoading) {
        return (
            <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
                <Loader2 className="h-5 w-5 animate-spin" />
                <span>Загрузка метаданных отчёта...</span>
            </div>
        )
    }

    if (!meta) {
        return (
            <div className="flex items-center justify-center h-64 gap-2 text-destructive">
                <AlertCircle className="h-5 w-5" />
                <span>Не удалось загрузить метаданные отчёта</span>
            </div>
        )
    }

    return (
        <div className="flex flex-col h-full">
            {/* Header */}
            <div className="px-4 py-3 border-b flex items-center justify-between">
                <div>
                    {headerSlot}
                    <h1 className="text-lg font-semibold">{meta.name}</h1>
                    {meta.description && (
                        <p className="text-sm text-muted-foreground">{meta.description}</p>
                    )}
                </div>
            </div>

            {/* Toolbar + Filters */}
            <div className="px-4 py-2 border-b flex items-center gap-2 flex-wrap">
                {/* Filter slot */}
                {filterSlot ?? <ReportFilterControls report={report} />}

                <div className="flex-1" />

                {/* View mode toggle [#16] */}
                <ViewModeToggle report={report} />

                {/* Grouping toggle */}
                {meta.groupBy.length > 0 && (
                    <GroupByDropdown report={report} />
                )}

                {/* Column chooser */}
                <ColumnChooserDropdown report={report} />

                {/* Field chooser (Query Engine mode) */}
                {report.availableFields && report.availableFields.length > 0 && (
                    <FieldChooserDropdown report={report} />
                )}

                {/* Variants [#14] */}
                <VariantsDropdown report={report} />

                {/* Share link [#15] */}
                <Button
                    variant="outline"
                    size="sm"
                    className="gap-1.5"
                    onClick={report.copyShareLink}
                    title="Копировать ссылку"
                >
                    <Link2 className="h-4 w-4" />
                </Button>

                {/* Export */}
                <Button
                    variant="outline"
                    size="sm"
                    className="gap-1.5"
                    disabled={report.status !== "done"}
                    onClick={() => report.exportReport()}
                >
                    <Download className="h-4 w-4" />
                    Экспорт
                </Button>

                {/* Generate button */}
                <Button
                    onClick={generate}
                    disabled={status === "loading"}
                    size="sm"
                    className="gap-1.5 relative"
                >
                    {report.isDirty && status !== "loading" && (
                        <span className="absolute -top-1 -right-1 flex h-3 w-3">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75"></span>
                            <span className="relative inline-flex rounded-full h-3 w-3 bg-amber-500"></span>
                        </span>
                    )}
                    {status === "loading" ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                        <Play className="h-4 w-4" />
                    )}
                    Сформировать
                </Button>
            </div>

            {/* Main content + FilterSidebar */}
            <div className="flex flex-1 overflow-hidden min-h-0">
                {/* Main content area */}
                <div className="flex-1 flex flex-col overflow-hidden min-h-0">
                    {/* Stale Data Warning */}
                    {report.isDirty && status !== "idle" && status !== "loading" && (
                        <div className="bg-amber-50/80 dark:bg-amber-950/40 border-b border-amber-200 dark:border-amber-900/50 px-4 py-2 text-sm text-amber-800 dark:text-amber-300 flex items-center justify-between shrink-0">
                            <div className="flex items-center gap-2">
                                <AlertCircle className="h-4 w-4" />
                                <span>Настройки отчета были изменены. Показаны устаревшие данные.</span>
                            </div>
                            <Button variant="ghost" size="sm" onClick={generate} className="h-6 text-amber-800 dark:text-amber-300 hover:bg-amber-100 dark:hover:bg-amber-900/50">
                                Обновить
                            </Button>
                        </div>
                    )}

                    {/* Content */}
                    <div className="flex-1 overflow-auto min-h-0 bg-muted/10 relative flex flex-col">
                        <ReportContent report={report} />
                    </div>
                </div>

                {/* Right sidebar: FilterSidebar (reused from entity lists) */}
                {report.filterFieldsMeta.length > 0 && (
                    <FilterSidebar
                        showGroups={false}
                        showDetails={false}
                        fieldsMeta={report.filterFieldsMeta}
                        onFilterValuesChange={report.onAdvancedFilterValuesChange}
                        initialFilterValues={report.advancedFilterValues}
                    />
                )}
            </div>
        </div>
    )
}

// ── Report Content ──────────────────────────────────────────────────────

function ReportContent({ report }: { report: UseReportPageReturn }) {
    const { status, error, displayRows, meta, totalItems, viewMode } = report

    switch (status) {
        case "idle":
            return (
                <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
                    <Play className="h-10 w-10 opacity-30" />
                    <p>Задайте параметры и нажмите «Сформировать»</p>
                </div>
            )
        case "loading":
            return (
                <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
                    <Loader2 className="h-5 w-5 animate-spin" />
                    <span>Формирование отчёта...</span>
                </div>
            )
        case "error":
            return (
                <div className="flex flex-col items-center justify-center h-64 gap-2 text-destructive">
                    <AlertCircle className="h-8 w-8" />
                    <p>{error ?? "Ошибка формирования отчёта"}</p>
                </div>
            )
        case "empty":
            return (
                <div className="flex flex-col items-center justify-center h-64 gap-2 text-muted-foreground">
                    <Inbox className="h-10 w-10 opacity-30" />
                    <p>Нет данных по заданным параметрам</p>
                </div>
            )
        case "export-only":
            return (
                <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
                    <Download className="h-10 w-10 opacity-30" />
                    <p className="text-center max-w-md">
                        Результат содержит более 50 000 строк и не может быть отображён интерактивно.
                        <br />
                        Используйте экспорт для получения данных.
                    </p>
                    <div className="flex gap-2 mt-2">
                        <Button
                            variant="outline"
                            size="sm"
                            className="gap-1.5"
                            onClick={() => report.exportReport()}
                        >
                            <Download className="h-4 w-4" />
                            Экспорт (Excel)
                        </Button>
                    </div>
                </div>
            )
        case "done":
            return (
                <>
                    <div className="px-4 py-1.5 text-xs text-muted-foreground border-b">
                        Найдено: {totalItems}
                    </div>
                    {viewMode === "chart" ? (
                        <ReportChartPlaceholder report={report} />
                    ) : (
                        <ReportTable
                            rows={displayRows}
                            columns={report.effectiveColumns}
                            visibleKeys={report.visibleColumnKeys}
                            sortColumn={report.sortColumn}
                            sortDirection={report.sortDirection}
                            onSort={report.handleSort}
                            onReorderColumn={report.reorderColumn}
                            selectionRange={report.selectionRange}
                            isDraggingSelection={report.isDraggingSelection}
                            onSelectionStart={report.onSelectionStart}
                            onSelectionMove={report.onSelectionMove}
                            onSelectionEnd={report.onSelectionEnd}
                        />
                    )}
                </>
            )
    }
}

// ── Report Table ────────────────────────────────────────────────────────

interface ReportTableProps {
    rows: DisplayRow[]
    columns: ReportColumnDef[]
    visibleKeys: string[]
    sortColumn: string | null
    sortDirection: "asc" | "desc"
    onSort: (key: string) => void
    onReorderColumn?: (fromIndex: number, toIndex: number) => void
    selectionRange: { start: { r: number, c: number }, end: { r: number, c: number } } | null
    isDraggingSelection: boolean
    onSelectionStart: (r: number, c: number) => void
    onSelectionMove: (r: number, c: number) => void
    onSelectionEnd: () => void
}

function ReportTable({
    rows,
    columns,
    visibleKeys,
    sortColumn,
    sortDirection,
    onSort,
    onReorderColumn,
    selectionRange,
    isDraggingSelection,
    onSelectionStart,
    onSelectionMove,
    onSelectionEnd,
}: ReportTableProps) {
    const visibleColumns = useMemo(() => {
        const colMap = new Map(columns.map((c) => [c.key, c]))
        return visibleKeys
            .map((k) => colMap.get(k))
            .filter((c): c is ReportColumnDef => c !== undefined)
    }, [columns, visibleKeys])

    // Track collapsed group indices
    const [collapsedGroups, setCollapsedGroups] = useState<Set<number>>(new Set())

    const toggleGroup = useCallback((groupIndex: number) => {
        setCollapsedGroups((prev) => {
            const next = new Set(prev)
            if (next.has(groupIndex)) {
                next.delete(groupIndex)
            } else {
                next.add(groupIndex)
            }
            return next
        })
    }, [])

    // Compute visible rows: skip children of collapsed groups
    const visibleRows = useMemo(() => {
        const result: { row: DisplayRow; originalIndex: number }[] = []
        let skipDepth: number | null = null

        for (let i = 0; i < rows.length; i++) {
            const row = rows[i]

            // If we're skipping children of a collapsed group
            if (skipDepth !== null) {
                // Footer rows have no depth — they end at the same level as data
                if (row.kind === "footer") {
                    skipDepth = null
                    result.push({ row, originalIndex: i })
                    continue
                }
                const rowDepth = row.kind === "group" ? row.depth : row.kind === "data" ? row.depth : row.kind === "subtotal" ? row.depth : 0
                
                // Skip children (greater depth) AND the subtotal row of the collapsed group itself
                if (rowDepth > skipDepth || (row.kind === "subtotal" && rowDepth === skipDepth)) {
                    continue // Skip this child
                } else {
                    skipDepth = null // End of collapsed region
                }
            }

            result.push({ row, originalIndex: i })

            // If this is a collapsed group, start skipping its children
            if (row.kind === "group" && collapsedGroups.has(i)) {
                skipDepth = row.depth
            }
        }
        return result
    }, [rows, collapsedGroups])

    const sensors = useSensors(
        useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
        useSensor(KeyboardSensor),
    )

    const handleDragEnd = useCallback((event: DragEndEvent) => {
        const { active, over } = event
        if (!over || active.id === over.id || !onReorderColumn) return
        const oldIndex = visibleKeys.indexOf(active.id as string)
        const newIndex = visibleKeys.indexOf(over.id as string)
        if (oldIndex !== -1 && newIndex !== -1) {
            onReorderColumn(oldIndex, newIndex)
        }
    }, [visibleKeys, onReorderColumn])

    useEffect(() => {
        if (isDraggingSelection) {
            window.addEventListener("mouseup", onSelectionEnd)
            return () => window.removeEventListener("mouseup", onSelectionEnd)
        }
    }, [isDraggingSelection, onSelectionEnd])

    const minR = selectionRange ? Math.min(selectionRange.start.r, selectionRange.end.r) : -1
    const maxR = selectionRange ? Math.max(selectionRange.start.r, selectionRange.end.r) : -1
    const minC = selectionRange ? Math.min(selectionRange.start.c, selectionRange.end.c) : -1
    const maxC = selectionRange ? Math.max(selectionRange.start.c, selectionRange.end.c) : -1

    // ── Ctrl+C: Copy selected cells to clipboard ─────────────────────
    useEffect(() => {
        function handleKeyDown(e: KeyboardEvent) {
            // Check for Ctrl+C. Use e.code="KeyC" to support Russian keyboard layout
            // where e.key is "с" (cyrillic). We also check e.key for broad compatibility.
            const isCopy = (e.ctrlKey || e.metaKey) && (e.code === "KeyC" || e.key.toLowerCase() === "c" || e.key.toLowerCase() === "с")
            if (!isCopy) return
            
            if (minR < 0 || maxR < 0 || minC < 0 || maxC < 0) return

            e.preventDefault()

            const selectedRows = visibleRows.slice(minR, maxR + 1)
            const selectedCols = visibleColumns.slice(minC, maxC + 1)

            const lines: string[] = []
            for (const { row } of selectedRows) {
                if (row.kind !== "data") continue
                const cells = selectedCols.map(col => {
                    const val = row.item[col.key]
                    const formatted = formatCellValue(val, col)
                    return String(formatted)
                })
                lines.push(cells.join("\t"))
            }
            if (lines.length === 0) return

            const tsv = lines.join("\n")

            // Try modern API first, fall back to execCommand
            if (navigator.clipboard?.writeText) {
                navigator.clipboard.writeText(tsv).then(
                    () => toast.success(`Скопировано ${lines.length} строк`),
                    () => fallbackCopy(tsv, lines.length),
                )
            } else {
                fallbackCopy(tsv, lines.length)
            }
        }

        function fallbackCopy(text: string, lineCount: number) {
            const ta = document.createElement("textarea")
            ta.value = text
            ta.style.position = "fixed"
            ta.style.left = "-9999px"
            document.body.appendChild(ta)
            ta.select()
            try {
                document.execCommand("copy")
                toast.success(`Скопировано ${lineCount} строк`)
            } catch {
                toast.error("Не удалось скопировать")
            }
            document.body.removeChild(ta)
        }

        window.addEventListener("keydown", handleKeyDown)
        return () => window.removeEventListener("keydown", handleKeyDown)
    }, [visibleRows, visibleColumns, minR, maxR, minC, maxC])

    const router = useRouter()

    return (
        <div className="flex flex-col flex-1 overflow-hidden min-h-0 bg-background">
            <ScrollArea className="flex-1">
            <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={handleDragEnd}
            >
                <table className="w-full text-sm select-none border-separate border-spacing-0">
                    <thead className="sticky top-0 bg-background border-b z-10">
                        <SortableContext
                            items={visibleKeys}
                            strategy={horizontalListSortingStrategy}
                        >
                            <tr>
                                {visibleColumns.map((col) => (
                                    <SortableColumnHeader
                                        key={col.key}
                                        column={col}
                                        isSorted={sortColumn === col.key}
                                        sortDirection={sortDirection}
                                        onSort={onSort}
                                    />
                                ))}
                            </tr>
                        </SortableContext>
                    </thead>
                <tbody>
                    {visibleRows.map(({ row, originalIndex }, rowIndex) => (
                        <ReportRow
                            key={originalIndex}
                            rowIndex={rowIndex}
                            row={row}
                            columns={visibleColumns}
                            isCollapsed={row.kind === "group" && collapsedGroups.has(originalIndex)}
                            onToggleGroup={row.kind === "group" ? () => toggleGroup(originalIndex) : undefined}
                            minR={minR}
                            maxR={maxR}
                            minC={minC}
                            maxC={maxC}
                            onSelectionStart={onSelectionStart}
                            onSelectionMove={onSelectionMove}
                            router={router}
                        />
                    ))}
                </tbody>
            </table>
            </DndContext>
            <ScrollBar orientation="horizontal" />
            </ScrollArea>
            
            <ReportStatusBar 
                selectionRange={selectionRange} 
                visibleRows={visibleRows} 
                visibleColumns={visibleColumns} 
            />
        </div>
    )
}

// ── Sortable Column Header ──────────────────────────────────────────────

function SortableColumnHeader({
    column,
    isSorted,
    sortDirection,
    onSort,
}: {
    column: ReportColumnDef
    isSorted: boolean
    sortDirection: "asc" | "desc"
    onSort: (key: string) => void
}) {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id: column.key })

    const style: React.CSSProperties = {
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.5 : 1,
        zIndex: isDragging ? 20 : undefined,
    }

    return (
        <th
            ref={setNodeRef}
            style={style}
            className={`px-3 py-2 font-medium text-muted-foreground select-none whitespace-nowrap ${
                column.align === "right" ? "text-right" : "text-left"
            } ${column.sortable ? "cursor-pointer hover:text-foreground" : ""}`}
            onClick={() => column.sortable && onSort(column.key)}
        >
            <span className="inline-flex items-center gap-1">
                <span
                    {...attributes}
                    {...listeners}
                    className="cursor-grab text-muted-foreground/40 hover:text-muted-foreground/80 touch-none"
                    onClick={(e) => e.stopPropagation()}
                    title="Перетащите для изменения порядка"
                >
                    ⠿
                </span>
                {column.label}
                {isSorted && (
                    <span className="text-xs">{sortDirection === "asc" ? "↑" : "↓"}</span>
                )}
            </span>
        </th>
    )
}

// ── Report Row (DisplayRow renderer) ────────────────────────────────────

function ReportRow({
    row,
    rowIndex,
    columns,
    isCollapsed,
    onToggleGroup,
    minR,
    maxR,
    minC,
    maxC,
    onSelectionStart,
    onSelectionMove,
    router,
}: {
    row: DisplayRow
    rowIndex: number
    columns: ReportColumnDef[]
    isCollapsed?: boolean
    onToggleGroup?: () => void
    minR: number
    maxR: number
    minC: number
    maxC: number
    onSelectionStart: (r: number, c: number) => void
    onSelectionMove: (r: number, c: number) => void
    router: ReturnType<typeof useRouter>
}) {
    switch (row.kind) {
        case "group":
            return (
                <tr
                    className="bg-muted/50 font-medium cursor-pointer hover:bg-muted/70 transition-colors"
                    onClick={onToggleGroup}
                >
                    <td colSpan={columns.length} className="px-3 py-1.5 border-b" style={{ paddingLeft: `${12 + row.depth * 20}px` }}>
                        <span className="inline-flex items-center gap-1.5">
                            {isCollapsed ? (
                                <ChevronRight className="h-3.5 w-3.5" />
                            ) : (
                                <ChevronDown className="h-3.5 w-3.5" />
                            )}
                            {row.label}
                            <Badge variant="secondary" className="text-xs font-normal">
                                {row.count}
                            </Badge>
                        </span>
                    </td>
                </tr>
            )
        case "data":
            return (
                <tr className="border-b transition-colors cursor-cell hover:bg-muted/30">
                    {columns.map((col, colIndex) => {
                        const isSelected = rowIndex >= minR && rowIndex <= maxR && colIndex >= minC && colIndex <= maxC
                        const isRef = col.type === "reference" && col.refRoute
                        return (
                            <td
                                key={col.key}
                                onMouseDown={(e) => {
                                    if (e.button !== 0) return // Only left click
                                    onSelectionStart(rowIndex, colIndex)
                                }}
                                onMouseEnter={() => onSelectionMove(rowIndex, colIndex)}
                                onDoubleClick={() => {
                                    if (isRef) {
                                        const uuid = row.item[col.refIdKey ?? ""] as string | undefined
                                        if (uuid) {
                                            router.push(`/catalogs/${col.refRoute}/${uuid}`)
                                        }
                                    }
                                }}
                                className={`px-3 py-1.5 ${col.align === "right" ? "text-right tabular-nums" : ""} ${
                                    isSelected ? "bg-primary/20 ring-1 ring-inset ring-primary/40" : ""
                                } ${isRef ? "hover:underline decoration-primary/50 cursor-pointer" : ""}`}
                                style={{ paddingLeft: col === columns[0] ? `${12 + row.depth * 20}px` : undefined }}
                            >
                                {formatCellValue(row.item[col.key], col)}
                            </td>
                        )
                    })}
                </tr>
            )
        case "subtotal": {
            // Find the first column that does NOT have a computed total — use it for the label
            const subtotalLabelIdx = columns.findIndex((col) => row.totals[col.key] === undefined)
            return (
                <tr className="bg-muted/30 font-medium text-sm border-b">
                    {columns.map((col, i) => (
                        <td
                            key={col.key}
                            className={`px-3 py-1 ${col.align === "right" ? "text-right tabular-nums" : ""}`}
                        >
                            {row.totals[col.key] !== undefined
                                ? formatNumber(row.totals[col.key])
                                : (i === subtotalLabelIdx ? "Итого по группе" : "")}
                        </td>
                    ))}
                </tr>
            )
        }
        case "footer": {
            const footerLabelIdx = columns.findIndex((col) => row.totals[col.key] === undefined)
            return (
                <tr className="bg-muted font-semibold border-t-2">
                    {columns.map((col, i) => (
                        <td
                            key={col.key}
                            className={`px-3 py-2 ${col.align === "right" ? "text-right tabular-nums" : ""}`}
                        >
                            {row.totals[col.key] !== undefined
                                ? formatNumber(row.totals[col.key])
                                : (i === footerLabelIdx ? "ИТОГО" : "")}
                        </td>
                    ))}
                </tr>
            )
        }
    }
}

// ── Chart Placeholder [#16] ─────────────────────────────────────────────

function ReportChartPlaceholder({ report }: { report: UseReportPageReturn }) {
    const { items, meta } = report

    // Find numeric columns for chart
    const numericColumns = (meta?.columns ?? []).filter(
        (c) => c.type === "quantity" || c.type === "money"
    )
    const labelColumn = (meta?.columns ?? []).find(
        (c) => c.type === "string" || c.type === "reference"
    )

    if (numericColumns.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center h-64 gap-2 text-muted-foreground">
                <BarChart3 className="h-10 w-10 opacity-30" />
                <p>Нет числовых колонок для построения диаграммы</p>
            </div>
        )
    }

    // Simple bar chart data (top 20 items)
    const chartData = items.slice(0, 20).map((item): { label: string; values: Record<string, number> } => ({
        label: String(item[labelColumn?.key ?? ""] ?? "—"),
        values: numericColumns.reduce<Record<string, number>>((acc, col) => {
            acc[col.key] = Number(item[col.key] ?? 0)
            return acc
        }, {}),
    }))

    // Render as a simple HTML bar chart (recharts can be added later)
    const maxValue = Math.max(
        ...chartData.flatMap((d) => numericColumns.map((c) => d.values[c.key] ?? 0)),
        1,
    )

    return (
        <div className="p-4 space-y-2">
            <p className="text-xs text-muted-foreground mb-3">
                Топ-20 • {numericColumns.map((c) => c.label).join(", ")}
            </p>
            {chartData.map((item, i) => (
                <div key={i} className="flex items-center gap-3 text-sm">
                    <span className="w-32 truncate text-right text-muted-foreground" title={item.label}>
                        {item.label}
                    </span>
                    <div className="flex-1 flex flex-col gap-0.5">
                        {numericColumns.map((col) => {
                            const value = item.values[col.key] ?? 0
                            const pct = (value / maxValue) * 100
                            return (
                                <div key={col.key} className="flex items-center gap-2">
                                    <div
                                        className="h-5 rounded-sm bg-primary/70 transition-all"
                                        style={{ width: `${Math.max(pct, 0.5)}%` }}
                                    />
                                    <span className="text-xs tabular-nums text-muted-foreground whitespace-nowrap">
                                        {value.toLocaleString("ru-RU", { maximumFractionDigits: 2 })}
                                    </span>
                                </div>
                            )
                        })}
                    </div>
                </div>
            ))}
        </div>
    )
}

// ── Report Status Bar (Aggregations) ────────────────────────────────────

function ReportStatusBar({ 
    selectionRange, 
    visibleRows, 
    visibleColumns 
}: { 
    selectionRange: { start: { r: number, c: number }, end: { r: number, c: number } } | null
    visibleRows: { row: DisplayRow; originalIndex: number }[]
    visibleColumns: ReportColumnDef[]
}) {
    if (!selectionRange) return null

    const minR = Math.min(selectionRange.start.r, selectionRange.end.r)
    const maxR = Math.max(selectionRange.start.r, selectionRange.end.r)
    const minC = Math.min(selectionRange.start.c, selectionRange.end.c)
    const maxC = Math.max(selectionRange.start.c, selectionRange.end.c)

    // Cell selection implies at least one cell. We don't need size > 1 restriction 
    // if the user deliberately selects a single cell, but usually we aggregate on >1.
    // Let's show it even for 1 cell if it's a measure, it helps verify values.

    const selectedCols = visibleColumns.slice(minC, maxC + 1)
    const aggregatableColumns = selectedCols.filter(
        c => c.type === "money" || c.type === "quantity"
    )

    if (aggregatableColumns.length === 0) return null

    const selectedItems = visibleRows
        .slice(minR, maxR + 1)
        .map(r => r.row)
        .filter(r => r.kind === "data")
        .map(r => r.item)

    if (selectedItems.length === 0) return null

    const aggregations = aggregatableColumns.map(col => {
        let sum = new Decimal(0)
        let count = 0
        for (const item of selectedItems) {
            const val = item[col.key]
            if (val !== undefined && val !== null && val !== "") {
                try {
                    sum = sum.plus(new Decimal(String(val)))
                    count++
                } catch {
                    // Ignore non-numeric values
                }
            }
        }
        const avg = count > 0 ? sum.dividedBy(count) : new Decimal(0)
        
        return {
            col,
            sum,
            avg,
            count
        }
    }).filter(agg => agg.count > 0)

    if (aggregations.length === 0) return null

    return (
        <div className="sticky bottom-0 bg-background border-t shadow-[0_-4px_6px_-1px_rgba(0,0,0,0.05)] z-20 flex flex-wrap gap-x-6 gap-y-2 items-center text-sm shrink-0 px-4 py-2 mt-auto">
            <div className="font-medium flex items-center gap-2 text-muted-foreground mr-2 border-r pr-4">
                <Table2 className="h-4 w-4" />
                Ячеек выделено: {selectedItems.length * selectedCols.length}
            </div>
            
            {aggregations.map(({ col, sum, avg, count }) => (
                <div key={col.key} className="flex items-center gap-4">
                    <span className="text-muted-foreground">{col.label}:</span>
                    <div className="flex items-center gap-3">
                        <span title="Сумма (Sum)" className="inline-flex items-center gap-1.5">
                            <span className="text-muted-foreground text-[10px] uppercase font-semibold">Sum</span>
                            <span className="font-medium tabular-nums">{sum.toNumber().toLocaleString("ru-RU", { maximumFractionDigits: 4 })}</span>
                        </span>
                        {count > 1 && (
                            <span title="Среднее (Avg)" className="inline-flex items-center gap-1.5 border-l pl-3">
                                <span className="text-muted-foreground text-[10px] uppercase font-semibold">Avg</span>
                                <span className="tabular-nums opacity-90">{avg.toNumber().toLocaleString("ru-RU", { maximumFractionDigits: 4 })}</span>
                            </span>
                        )}
                        <Badge variant="outline" className="text-[10px] font-normal h-5 px-1.5 border-dashed" title="Количество значений (Count)">
                            {count}
                        </Badge>
                    </div>
                </div>
            ))}
        </div>
    )
}

// ── Formatting helpers ──────────────────────────────────────────────────

function formatCellValue(value: unknown, col: ReportColumnDef): React.ReactNode {
    if (value === undefined || value === null) return "—"

    switch (col.type) {
        case "quantity":
        case "money":
            return typeof value === "number"
                ? value.toLocaleString("ru-RU", { maximumFractionDigits: 4 })
                : String(value)
        case "boolean":
            return value ? "✓" : "—"
        case "date":
            if (typeof value === "string") {
                const d = new Date(value)
                return isNaN(d.getTime()) ? value : d.toLocaleDateString("ru-RU")
            }
            return String(value)
        default:
            return String(value)
    }
}

function formatNumber(value: number): string {
    return value.toLocaleString("ru-RU", { maximumFractionDigits: 4 })
}

// ── Toolbar Dropdowns ───────────────────────────────────────────────────

function ViewModeToggle({ report }: { report: UseReportPageReturn }) {
    return (
        <div className="inline-flex rounded-md border" role="group">
            <Button
                variant={report.viewMode === "table" ? "secondary" : "ghost"}
                size="sm"
                className="rounded-r-none border-r gap-1"
                onClick={() => report.setViewMode("table")}
            >
                <Table2 className="h-4 w-4" />
            </Button>
            <Button
                variant={report.viewMode === "chart" ? "secondary" : "ghost"}
                size="sm"
                className="rounded-l-none gap-1"
                onClick={() => report.setViewMode("chart")}
            >
                <BarChart3 className="h-4 w-4" />
            </Button>
        </div>
    )
}

function GroupByDropdown({ report }: { report: UseReportPageReturn }) {
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="gap-1.5">
                    <Layers className="h-4 w-4" />
                    Группировка
                    {report.activeGroupBy.length > 0 && (
                        <Badge variant="secondary" className="text-xs ml-1">
                            {report.activeGroupBy.length}
                        </Badge>
                    )}
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
                {report.meta?.groupBy.map((g) => (
                    <DropdownMenuCheckboxItem
                        key={g.key}
                        checked={report.activeGroupBy.includes(g.key)}
                        onCheckedChange={() => report.toggleGroupBy(g.key)}
                    >
                        {g.label}
                    </DropdownMenuCheckboxItem>
                ))}
            </DropdownMenuContent>
        </DropdownMenu>
    )
}

function ColumnChooserDropdown({ report }: { report: UseReportPageReturn }) {
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="gap-1.5">
                    <Columns3 className="h-4 w-4" />
                    Колонки
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="max-h-[300px] overflow-y-auto">
                {report.effectiveColumns.map((col) => (
                    <DropdownMenuCheckboxItem
                        key={col.key}
                        checked={report.visibleColumnKeys.includes(col.key)}
                        onCheckedChange={() => report.toggleColumn(col.key)}
                    >
                        {col.label}
                    </DropdownMenuCheckboxItem>
                ))}
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={report.resetColumns}>
                    Сбросить по умолчанию
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    )
}



// ── Report Variants Dropdown [#14] ──────────────────────────────────────

import type { VariantVisibility } from "@/types/report-variant"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Checkbox } from "@/components/ui/checkbox"

function VariantsDropdown({ report }: { report: UseReportPageReturn }) {
    const [saveOpen, setSaveOpen] = useState(false)
    const [variantName, setVariantName] = useState("")
    const [visibility, setVisibility] = useState<VariantVisibility>("personal")
    const [isDefault, setIsDefault] = useState(false)

    const activeVariant = report.variants.find((v) => v.id === report.activeVariantId)

    // Pre-fill form fields from active variant when opening dialog
    const openSaveDialog = useCallback(() => {
        if (activeVariant) {
            setVariantName(activeVariant.name)
            setVisibility(activeVariant.visibility as VariantVisibility)
            setIsDefault(activeVariant.isDefault)
        } else {
            setVariantName("")
            setVisibility("personal")
            setIsDefault(false)
        }
        setSaveOpen(true)
    }, [activeVariant])

    const handleSave = async () => {
        const name = variantName.trim()
        if (!name) return

        // If the active variant exists and name hasn't changed — overwrite (update)
        if (activeVariant && name === activeVariant.name) {
            await report.updateVariant(activeVariant.id, name, visibility, isDefault)
        } else {
            await report.saveVariant(name, visibility, isDefault)
        }
        setSaveOpen(false)
    }

    return (
        <>
            <DropdownMenu>
                <DropdownMenuTrigger asChild>
                    <Button variant="outline" size="sm" className="gap-1.5">
                        <BookmarkCheck className="h-4 w-4" />
                        Варианты
                        {activeVariant && (
                            <Badge variant="secondary" className="text-xs ml-1 max-w-[100px] truncate">
                                {activeVariant.name}
                            </Badge>
                        )}
                    </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-56">
                    <DropdownMenuLabel>Сохранённые варианты</DropdownMenuLabel>
                    <DropdownMenuSeparator />
                    {report.variants.length === 0 ? (
                        <div className="px-2 py-3 text-xs text-center text-muted-foreground">
                            Нет сохранённых вариантов
                        </div>
                    ) : (
                        report.variants.map((v) => (
                            <DropdownMenuItem
                                key={v.id}
                                className="flex items-center justify-between"
                                onClick={() => report.loadVariant(v.id)}
                            >
                                <span className="truncate">
                                    {v.name}
                                    {v.visibility === "shared" && <Badge variant="outline" className="ml-2 text-[10px] uppercase">Общий</Badge>}
                                    {v.visibility === "system" && <Badge variant="outline" className="ml-2 text-[10px] uppercase border-primary text-primary">Системный</Badge>}
                                </span>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-5 w-5 shrink-0 ml-2"
                                    onClick={(e) => {
                                        e.stopPropagation()
                                        report.deleteVariant(v.id)
                                    }}
                                >
                                    <Trash2 className="h-3 w-3 text-muted-foreground" />
                                </Button>
                            </DropdownMenuItem>
                        ))
                    )}
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onClick={openSaveDialog}>
                        <Save className="h-4 w-4 mr-2" />
                        Сохранить текущий…
                    </DropdownMenuItem>
                </DropdownMenuContent>
            </DropdownMenu>

            {/* Save Variant Dialog */}
            <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
                <DialogContent className="sm:max-w-[360px]">
                    <DialogHeader>
                        <DialogTitle>Сохранить вариант отчёта</DialogTitle>
                    </DialogHeader>
                    <div className="py-3 space-y-4">
                        <div className="space-y-2">
                            <label className="text-sm font-medium">Название варианта</label>
                            <Input
                                placeholder="Мой вариант"
                                value={variantName}
                                onChange={(e) => setVariantName(e.target.value)}
                                onKeyDown={(e) => e.key === "Enter" && handleSave()}
                                autoFocus
                            />
                        </div>
                        <div className="space-y-2">
                            <label className="text-sm font-medium">Доступность</label>
                            <Select value={visibility} onValueChange={(val: VariantVisibility) => setVisibility(val)}>
                                <SelectTrigger>
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value="personal">Личный (только для меня)</SelectItem>
                                    <SelectItem value="shared">Общий (для всех пользователей)</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="flex items-center space-x-2">
                            <Checkbox id="is-default-variant" checked={isDefault} onCheckedChange={(c) => setIsDefault(!!c)} />
                            <label htmlFor="is-default-variant" className="text-sm font-medium leading-none cursor-pointer">
                                Использовать по умолчанию
                            </label>
                        </div>
                    </div>
                    <DialogFooter>
                        <Button variant="outline" size="sm" onClick={() => setSaveOpen(false)}>
                            Отменить
                        </Button>
                        <Button size="sm" onClick={handleSave} disabled={!variantName.trim()}>
                            Сохранить
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    )
}

// ── Auto-generated Filter Controls (from metadata) ──────────────────────

function ReportFilterControls({ report }: { report: UseReportPageReturn }) {
    const { meta, filterValues, setFilterValue } = report
    if (!meta) return null

    return (
        <div className="flex items-center gap-2 flex-wrap">
            {meta.filters.map((filter) => {
                switch (filter.type) {
                    case "date":
                        return (
                            <div key={filter.key} className="flex items-center gap-2">
                                <span className="text-sm text-muted-foreground">{filter.label}</span>
                                <DatePicker
                                    value={filterValues[filter.key] ? new Date(String(filterValues[filter.key])) : undefined}
                                    onChange={(date) => setFilterValue(filter.key, date ? format(date, "yyyy-MM-dd") : undefined)}
                                    className="w-[140px]"
                                />
                            </div>
                        )
                    case "boolean":
                        return (
                            <label key={filter.key} className="inline-flex items-center gap-1.5 text-sm cursor-pointer">
                                <input
                                    type="checkbox"
                                    checked={filterValues[filter.key] === true || filterValues[filter.key] === "true"}
                                    onChange={(e) => setFilterValue(filter.key, e.target.checked || undefined)}
                                    className="rounded border"
                                />
                                {filter.label}
                            </label>
                        )
                    case "string":
                        return (
                            <input
                                key={filter.key}
                                type="text"
                                placeholder={filter.label}
                                className="h-8 px-2 text-sm border rounded-md bg-background w-40"
                                value={String(filterValues[filter.key] ?? "")}
                                onChange={(e) => setFilterValue(filter.key, e.target.value || undefined)}
                            />
                        )
                    default:
                        // reference, enum, period — will be enhanced later
                        return null
                }
            })}
        </div>
    )
}

// ── Field Chooser (Query Engine) ────────────────────────────────────────

function FieldChooserDropdown({ report }: { report: UseReportPageReturn }) {
    const { availableFields, selectedFields, toggleField } = report
    if (!availableFields) return null

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="gap-1.5">
                    <ListTree className="h-4 w-4" />
                    Поля
                    <Badge variant="secondary" className="ml-1 text-xs">
                        {selectedFields.length}
                    </Badge>
                    <ChevronDown className="h-3 w-3 opacity-50" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-72 max-h-96 overflow-y-auto">
                <DropdownMenuLabel>Доступные поля</DropdownMenuLabel>
                <DropdownMenuSeparator />
                {availableFields.map((node) => (
                    <FieldTreeItem
                        key={node.key}
                        node={node}
                        selectedFields={selectedFields}
                        toggleField={toggleField}
                        depth={0}
                    />
                ))}
            </DropdownMenuContent>
        </DropdownMenu>
    )
}

FieldChooserDropdown.displayName = "FieldChooserDropdown"

function FieldTreeItem({
    node,
    selectedFields,
    toggleField,
    depth,
}: {
    node: FieldTreeNode
    selectedFields: string[]
    toggleField: (key: string) => void
    depth: number
}) {
    const [expanded, setExpanded] = useState(false)
    const hasChildren = node.children && node.children.length > 0
    // For root ref nodes, "selected" means the .name path is in selectedFields
    // (e.g. node.key="warehouse_id" → check for "warehouse_id.name")
    const effectiveKey = (node.type === "ref" && !node.key.includes("."))
        ? node.key + ".name"
        : node.key
    const isSelected = selectedFields.includes(effectiveKey)

    return (
        <div>
            <div
                className="flex items-center gap-1 px-2 py-1 hover:bg-accent rounded-sm cursor-pointer text-sm"
                style={{ paddingLeft: `${8 + depth * 16}px` }}
            >
                {/* Expand/collapse for ref nodes */}
                {hasChildren ? (
                    <button
                        onClick={(e) => { e.stopPropagation(); setExpanded(!expanded) }}
                        className="p-0.5 hover:bg-muted rounded"
                    >
                        {expanded
                            ? <ChevronDown className="h-3 w-3" />
                            : <ChevronRight className="h-3 w-3" />
                        }
                    </button>
                ) : (
                    <span className="w-4" />
                )}

                {/* Checkbox */}
                <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => toggleField(node.key)}
                    className="rounded border"
                />

                {/* Label */}
                <span
                    className="flex-1 truncate"
                    onClick={() => toggleField(node.key)}
                >
                    {node.label}
                </span>

                {/* Type badge */}
                <span className="text-xs text-muted-foreground">
                    {node.type === "ref" ? "↗" : ""}
                </span>
            </div>

            {/* Children */}
            {hasChildren && expanded && node.children!.map((child) => (
                <FieldTreeItem
                    key={child.key}
                    node={child}
                    selectedFields={selectedFields}
                    toggleField={toggleField}
                    depth={depth + 1}
                />
            ))}
        </div>
    )
}

FieldTreeItem.displayName = "FieldTreeItem"

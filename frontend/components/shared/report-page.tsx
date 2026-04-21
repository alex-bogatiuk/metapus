"use client"

/**
 * ReportPage — generic container for all metadata-driven reports.
 *
 * Wave 1: Core layout (toolbar, filter controls, data table)
 * Wave 3: Drill-down panel, report variants, URL sharing, view toggle
 *
 * No report-specific code — driven entirely by useReportPage hook.
 */

import React, { useMemo, useState } from "react"
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
    ChevronDown, Link2, Save, BookmarkCheck, Trash2, Table2, BarChart3, X,
} from "lucide-react"
import type { UseReportPageReturn } from "@/hooks/useReportPage"
import type { DisplayRow, ReportColumnDef } from "@/types/report-meta"

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
                {meta.exportFormats.length > 0 && (
                    <ExportDropdown report={report} />
                )}

                {/* Generate button */}
                <Button
                    onClick={generate}
                    disabled={status === "loading"}
                    size="sm"
                    className="gap-1.5"
                >
                    {status === "loading" ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                        <Play className="h-4 w-4" />
                    )}
                    Сформировать
                </Button>
            </div>

            {/* Content */}
            <div className="flex-1 overflow-auto relative">
                <ReportContent report={report} />
            </div>

            {/* Drill-down panel [#13] */}
            <DrillDownPanel report={report} />
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
                            columns={meta?.columns ?? []}
                            visibleKeys={report.visibleColumnKeys}
                            sortColumn={report.sortColumn}
                            sortDirection={report.sortDirection}
                            onSort={report.handleSort}
                            onRowClick={report.selectRow}
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
    onRowClick?: (row: Record<string, unknown> | null) => void
}

function ReportTable({
    rows,
    columns,
    visibleKeys,
    sortColumn,
    sortDirection,
    onSort,
    onRowClick,
}: ReportTableProps) {
    const visibleColumns = useMemo(() => {
        const colMap = new Map(columns.map((c) => [c.key, c]))
        return visibleKeys
            .map((k) => colMap.get(k))
            .filter((c): c is ReportColumnDef => c !== undefined)
    }, [columns, visibleKeys])

    return (
        <div className="overflow-auto">
            <table className="w-full text-sm">
                <thead className="sticky top-0 bg-background border-b z-10">
                    <tr>
                        {visibleColumns.map((col) => (
                            <th
                                key={col.key}
                                className={`px-3 py-2 font-medium text-muted-foreground cursor-pointer hover:text-foreground select-none whitespace-nowrap ${
                                    col.align === "right" ? "text-right" : "text-left"
                                }`}
                                onClick={() => col.sortable && onSort(col.key)}
                            >
                                <span className="inline-flex items-center gap-1">
                                    {col.label}
                                    {sortColumn === col.key && (
                                        <span className="text-xs">{sortDirection === "asc" ? "↑" : "↓"}</span>
                                    )}
                                </span>
                            </th>
                        ))}
                    </tr>
                </thead>
                <tbody>
                    {rows.map((row, idx) => (
                        <ReportRow key={idx} row={row} columns={visibleColumns} onRowClick={onRowClick} />
                    ))}
                </tbody>
            </table>
        </div>
    )
}

// ── Report Row (DisplayRow renderer) ────────────────────────────────────

function ReportRow({
    row,
    columns,
    onRowClick,
}: {
    row: DisplayRow
    columns: ReportColumnDef[]
    onRowClick?: (row: Record<string, unknown> | null) => void
}) {
    switch (row.kind) {
        case "group":
            return (
                <tr className="bg-muted/50 font-medium">
                    <td colSpan={columns.length} className="px-3 py-1.5" style={{ paddingLeft: `${12 + row.depth * 20}px` }}>
                        <span className="inline-flex items-center gap-1.5">
                            <ChevronDown className="h-3.5 w-3.5" />
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
                <tr
                    className="border-b hover:bg-muted/30 transition-colors cursor-pointer"
                    onClick={() => onRowClick?.(row.item)}
                >
                    {columns.map((col) => (
                        <td
                            key={col.key}
                            className={`px-3 py-1.5 ${col.align === "right" ? "text-right tabular-nums" : ""}`}
                            style={{ paddingLeft: col === columns[0] ? `${12 + row.depth * 20}px` : undefined }}
                        >
                            {formatCellValue(row.item[col.key], col)}
                        </td>
                    ))}
                </tr>
            )
        case "subtotal":
            return (
                <tr className="bg-muted/30 font-medium text-sm border-b">
                    {columns.map((col, i) => (
                        <td
                            key={col.key}
                            className={`px-3 py-1 ${col.align === "right" ? "text-right tabular-nums" : ""}`}
                        >
                            {i === 0 ? "Итого по группе" : (row.totals[col.key] !== undefined ? formatNumber(row.totals[col.key]) : "")}
                        </td>
                    ))}
                </tr>
            )
        case "footer":
            return (
                <tr className="bg-muted font-semibold border-t-2">
                    {columns.map((col, i) => (
                        <td
                            key={col.key}
                            className={`px-3 py-2 ${col.align === "right" ? "text-right tabular-nums" : ""}`}
                        >
                            {i === 0 ? "ИТОГО" : (row.totals[col.key] !== undefined ? formatNumber(row.totals[col.key]) : "")}
                        </td>
                    ))}
                </tr>
            )
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

// ── Drill-Down Panel [#13] ──────────────────────────────────────────────

function DrillDownPanel({ report }: { report: UseReportPageReturn }) {
    const { selectedRow, selectRow, meta } = report

    if (!selectedRow || !meta) return null

    const visibleColumns = meta.columns.filter((c) => !c.defaultHidden)

    return (
        <Sheet open={!!selectedRow} onOpenChange={(open) => { if (!open) selectRow(null) }}>
            <SheetContent side="right" className="w-[400px] sm:w-[480px]">
                <SheetHeader>
                    <SheetTitle className="flex items-center gap-2">
                        Детали строки
                        <Button variant="ghost" size="icon" className="h-6 w-6 ml-auto" onClick={() => selectRow(null)}>
                            <X className="h-4 w-4" />
                        </Button>
                    </SheetTitle>
                </SheetHeader>
                <div className="mt-4 space-y-3">
                    {meta.columns.map((col) => {
                        const value = selectedRow[col.key]
                        if (value === undefined || value === null) return null
                        return (
                            <div key={col.key} className="grid grid-cols-[140px_1fr] gap-2 text-sm">
                                <span className="text-muted-foreground font-medium truncate">{col.label}</span>
                                <span className={col.align === "right" ? "text-right tabular-nums" : ""}>
                                    {formatCellValue(value, col)}
                                </span>
                            </div>
                        )
                    })}
                </div>
            </SheetContent>
        </Sheet>
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
                {report.meta?.columns.map((col) => (
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

function ExportDropdown({ report }: { report: UseReportPageReturn }) {
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="gap-1.5" disabled={report.status !== "done"}>
                    <Download className="h-4 w-4" />
                    Экспорт
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
                {report.meta?.exportFormats.map((fmt) => (
                    <DropdownMenuItem key={fmt} onClick={() => report.exportReport(fmt)}>
                        {fmt.toUpperCase()}
                    </DropdownMenuItem>
                ))}
            </DropdownMenuContent>
        </DropdownMenu>
    )
}

// ── Report Variants Dropdown [#14] ──────────────────────────────────────

function VariantsDropdown({ report }: { report: UseReportPageReturn }) {
    const [saveOpen, setSaveOpen] = useState(false)
    const [variantName, setVariantName] = useState("")

    const handleSave = () => {
        if (variantName.trim()) {
            report.saveVariant(variantName.trim())
            setVariantName("")
            setSaveOpen(false)
        }
    }

    return (
        <>
            <DropdownMenu>
                <DropdownMenuTrigger asChild>
                    <Button variant="outline" size="sm" className="gap-1.5">
                        <BookmarkCheck className="h-4 w-4" />
                        Варианты
                        {report.activeVariantName && (
                            <Badge variant="secondary" className="text-xs ml-1 max-w-[100px] truncate">
                                {report.activeVariantName}
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
                                key={v.name}
                                className="flex items-center justify-between"
                                onClick={() => report.loadVariant(v.name)}
                            >
                                <span className="truncate">{v.name}</span>
                                <Button
                                    variant="ghost"
                                    size="icon"
                                    className="h-5 w-5 shrink-0 ml-2"
                                    onClick={(e) => {
                                        e.stopPropagation()
                                        report.deleteVariant(v.name)
                                    }}
                                >
                                    <Trash2 className="h-3 w-3 text-muted-foreground" />
                                </Button>
                            </DropdownMenuItem>
                        ))
                    )}
                    <DropdownMenuSeparator />
                    <DropdownMenuItem onClick={() => setSaveOpen(true)}>
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
                    <div className="py-3">
                        <Input
                            placeholder="Название варианта"
                            value={variantName}
                            onChange={(e) => setVariantName(e.target.value)}
                            onKeyDown={(e) => e.key === "Enter" && handleSave()}
                            autoFocus
                        />
                    </div>
                    <DialogFooter>
                        <Button variant="outline" size="sm" onClick={() => setSaveOpen(false)}>
                            Отмена
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
                            <input
                                key={filter.key}
                                type="date"
                                placeholder={filter.label}
                                className="h-8 px-2 text-sm border rounded-md bg-background"
                                value={String(filterValues[filter.key] ?? "")}
                                onChange={(e) => setFilterValue(filter.key, e.target.value || undefined)}
                                title={filter.label}
                            />
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

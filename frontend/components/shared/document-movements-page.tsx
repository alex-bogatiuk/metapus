"use client"

/**
 * DocumentMovementsPage — full-page view of register movements for a document.
 *
 * Designed for accountants and technical specialists who need to audit
 * how a document affected registers. Key UX features:
 * - shadcn Tabs: one tab per register + "All movements" tab (1С-style)
 * - Excel-like cell selection with drag-select (ported from report-page)
 * - Ctrl+C → TSV clipboard copy
 * - Sum/Avg/Count status bar for selected numeric cells
 * - Footer totals row per register
 * - Sortable columns (click header to sort)
 *
 * Analogous to:
 * - 1С: "Движения документа" (Ctrl+F11) with TabControl
 * - SAP: MB03 → Accounting tab with ALV grid cell selection
 */

import { useState, useEffect, useMemo, useCallback } from "react"
import Link from "next/link"
import { ArrowLeft, Loader2, ArrowUpRight, ArrowDownRight, ArrowUpDown, ArrowUp, ArrowDown } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"
import { fmtAmount } from "@/lib/format"
import { toast } from "sonner"
import Decimal from "decimal.js"
import type { DocumentMovementsResponse, DocumentMovement, MovementColumnDef, MovementRefValue } from "@/types/common"

// ── Constants ───────────────────────────────────────────────────────────

const ALL_TAB_KEY = "__all__"

// ── Props ───────────────────────────────────────────────────────────────

interface DocumentMovementsPageProps {
    documentId: string
    backHref: string
    documentTitle: string
    fetcher: (id: string) => Promise<DocumentMovementsResponse>
}

// ── Selection types ─────────────────────────────────────────────────────

interface CellCoord { r: number; c: number }
interface SelectionRange { start: CellCoord; end: CellCoord }

type SortDir = "asc" | "desc" | null
interface SortState { column: string; dir: SortDir }

// ── Main Component ──────────────────────────────────────────────────────

export function DocumentMovementsPage({ documentId, backHref, documentTitle, fetcher }: DocumentMovementsPageProps) {
    const [movements, setMovements] = useState<DocumentMovement[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        let isMounted = true
        void Promise.resolve().then(() => {
            if (!isMounted) return
            setLoading(true)
            fetcher(documentId)
                .then((res) => { if (isMounted) setMovements(res.movements ?? []) })
                .catch((e) => { if (isMounted) setError(e instanceof Error ? e.message : "Не удалось загрузить данные") })
                .finally(() => { if (isMounted) setLoading(false) })
        })
        return () => { isMounted = false }
    }, [documentId, fetcher])

    // Group movements by registerName
    const grouped = useMemo(() => {
        return movements.reduce<Record<string, DocumentMovement[]>>((acc, m) => {
            if (!acc[m.registerName]) acc[m.registerName] = []
            acc[m.registerName].push(m)
            return acc
        }, {})
    }, [movements])

    const registerNames = useMemo(() => Object.keys(grouped), [grouped])

    return (
        <div className="flex h-full flex-col">
            {/* Header */}
            <div className="border-b bg-card sticky top-0 z-20 shadow-sm">
                <div className="flex items-center gap-2 px-4 py-2">
                    <Button variant="ghost" size="icon" className="h-7 w-7" asChild>
                        <Link href={backHref}>
                            <ArrowLeft className="h-3.5 w-3.5" />
                        </Link>
                    </Button>
                    <h1 className="text-sm font-semibold">Движения документа</h1>
                    <Badge variant="secondary" className="h-6 rounded-full px-2.5 text-[11px] font-medium">
                        {documentTitle}
                    </Badge>
                    <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                        Всего записей: {movements.length}
                    </span>
                </div>
            </div>

            {/* Loading */}
            {loading && (
                <div className="flex flex-1 items-center justify-center">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
            )}

            {/* Error */}
            {error && (
                <div className="flex flex-1 items-center justify-center text-sm text-destructive">
                    {error}
                </div>
            )}

            {/* Empty */}
            {!loading && !error && movements.length === 0 && (
                <div className="flex flex-1 flex-col items-center justify-center text-muted-foreground">
                    <span className="text-sm">Документ не проведён или не содержит движений</span>
                </div>
            )}

            {/* Content with Tabs */}
            {!loading && !error && movements.length > 0 && (
                <Tabs defaultValue={ALL_TAB_KEY} className="flex flex-1 flex-col overflow-hidden min-h-0">
                    <div className="px-4 border-b bg-card">
                        <TabsList variant="line" className="h-9">
                            <TabsTrigger variant="line" value={ALL_TAB_KEY} className="text-xs gap-1.5">
                                Все движения
                                <Badge variant="secondary" className="text-[10px] h-4 px-1.5 rounded-full">
                                    {movements.length}
                                </Badge>
                            </TabsTrigger>
                            {registerNames.map(name => (
                                <TabsTrigger key={name} variant="line" value={name} className="text-xs gap-1.5">
                                    {name}
                                    <Badge variant="secondary" className="text-[10px] h-4 px-1.5 rounded-full">
                                        {grouped[name].length}
                                    </Badge>
                                </TabsTrigger>
                            ))}
                        </TabsList>
                    </div>

                    {/* All movements tab */}
                    <TabsContent value={ALL_TAB_KEY} className="flex-1 overflow-hidden mt-0 min-h-0">
                        <ScrollArea className="h-full">
                            <div className="space-y-0">
                                {registerNames.map(name => (
                                    <RegisterGroupTable
                                        key={name}
                                        registerName={name}
                                        items={grouped[name]}
                                        showHeader
                                        fillHeight={false}
                                    />
                                ))}
                            </div>
                        </ScrollArea>
                    </TabsContent>

                    {/* Per-register tabs */}
                    {registerNames.map(name => (
                        <TabsContent key={name} value={name} className="flex-1 overflow-hidden mt-0 min-h-0">
                            <ScrollArea className="h-full">
                                <RegisterGroupTable
                                    registerName={name}
                                    items={grouped[name]}
                                    showHeader={false}
                                    fillHeight={false}
                                />
                            </ScrollArea>
                        </TabsContent>
                    ))}
                </Tabs>
            )}
        </div>
    )
}

// ── Register Group Table ────────────────────────────────────────────────

function RegisterGroupTable({
    registerName,
    items,
    showHeader,
    fillHeight = true,
}: {
    registerName: string
    items: DocumentMovement[]
    showHeader: boolean
    /** When true, table fills available height (flex-1). When false, uses auto height. */
    fillHeight?: boolean
}) {
    const [sort, setSort] = useState<SortState>({ column: "", dir: null })

    // Selection state (Excel-like drag)
    const [selectionRange, setSelectionRange] = useState<SelectionRange | null>(null)
    const [isDragging, setIsDragging] = useState(false)

    const columns = useMemo(() => {
        if (!items.length) return []
        return items[0].columns || []
    }, [items])

    // Extract currencyId from first item for amount formatting
    const currencyId = useMemo(() => {
        if (!items.length) return undefined
        const raw = items[0].data["currency"]
        const ref = (typeof raw === "object" && raw !== null && "id" in raw) ? raw as MovementRefValue : undefined
        return ref?.id
    }, [items])
    const { decimalPlaces } = useCurrencyScale(currencyId)

    const handleSort = useCallback((col: string) => {
        setSort((prev) => {
            if (prev.column !== col) return { column: col, dir: "asc" }
            if (prev.dir === "asc") return { column: col, dir: "desc" }
            return { column: "", dir: null }
        })
    }, [])

    const sorted = useMemo(() => {
        if (!sort.column || !sort.dir) return items
        const colDef = columns.find(c => c.key === sort.column)
        if (!colDef) return items

        return [...items].sort((a, b) => {
            const va = a.data[sort.column]
            const vb = b.data[sort.column]

            if (colDef.type === "amount" || colDef.type === "quantity") {
                const na = Number(va) || 0
                const nb = Number(vb) || 0
                return sort.dir === "asc" ? na - nb : nb - na
            }

            if (colDef.type === "ref") {
                const sa = (typeof va === "object" && va !== null && "name" in va) ? (va as MovementRefValue).name : ""
                const sb = (typeof vb === "object" && vb !== null && "name" in vb) ? (vb as MovementRefValue).name : ""
                return sort.dir === "asc" ? sa.localeCompare(sb) : sb.localeCompare(sa)
            }

            const sa = String(va ?? "")
            const sb = String(vb ?? "")
            return sort.dir === "asc" ? sa.localeCompare(sb) : sb.localeCompare(sa)
        })
    }, [items, sort, columns])

    const receipts = items.filter(m => m.recordType === "receipt").length
    const expenses = items.filter(m => m.recordType === "expense").length

    // ── Footer totals ───────────────────────────────────────────────────
    const footerTotals = useMemo(() => {
        const totals: Record<string, number> = {}
        for (const col of columns) {
            if (col.type !== "amount" && col.type !== "quantity") continue
            let sum = 0
            for (const m of items) {
                sum += Number(m.data[col.key]) || 0
            }
            totals[col.key] = sum
        }
        return totals
    }, [items, columns])

    const hasFooter = Object.keys(footerTotals).length > 0

    // ── Selection handlers ──────────────────────────────────────────────
    const onSelectionStart = useCallback((r: number, c: number) => {
        setSelectionRange({ start: { r, c }, end: { r, c } })
        setIsDragging(true)
    }, [])

    const onSelectionMove = useCallback((r: number, c: number) => {
        if (!isDragging) return
        setSelectionRange(prev => prev ? { ...prev, end: { r, c } } : null)
    }, [isDragging])

    const onSelectionEnd = useCallback(() => {
        setIsDragging(false)
    }, [])

    // Global mouseup listener for drag end
    useEffect(() => {
        if (isDragging) {
            window.addEventListener("mouseup", onSelectionEnd)
            return () => window.removeEventListener("mouseup", onSelectionEnd)
        }
    }, [isDragging, onSelectionEnd])

    // Selection bounds
    const minR = selectionRange ? Math.min(selectionRange.start.r, selectionRange.end.r) : -1
    const maxR = selectionRange ? Math.max(selectionRange.start.r, selectionRange.end.r) : -1
    const minC = selectionRange ? Math.min(selectionRange.start.c, selectionRange.end.c) : -1
    const maxC = selectionRange ? Math.max(selectionRange.start.c, selectionRange.end.c) : -1

    // ── Ctrl+C: Copy selected cells ─────────────────────────────────────
    useEffect(() => {
        function handleKeyDown(e: KeyboardEvent) {
            const isCopy = (e.ctrlKey || e.metaKey) && (e.code === "KeyC" || e.key.toLowerCase() === "c" || e.key.toLowerCase() === "с")
            if (!isCopy || minR < 0) return

            e.preventDefault()

            const selectedRows = sorted.slice(minR, maxR + 1)
            const selectedCols = columns.slice(minC, maxC + 1)

            const lines: string[] = []
            for (const m of selectedRows) {
                const cells = selectedCols.map(col => formatCellRaw(m.data[col.key], col, decimalPlaces))
                lines.push(cells.join("\t"))
            }
            if (lines.length === 0) return

            const tsv = lines.join("\n")
            if (navigator.clipboard?.writeText) {
                navigator.clipboard.writeText(tsv).then(
                    () => toast.success(`Скопировано ${lines.length} строк`),
                    () => toast.error("Не удалось скопировать"),
                )
            }
        }

        window.addEventListener("keydown", handleKeyDown)
        return () => window.removeEventListener("keydown", handleKeyDown)
    }, [sorted, columns, minR, maxR, minC, maxC, decimalPlaces])

    return (
        <div className={fillHeight ? "flex flex-col flex-1 overflow-hidden min-h-0" : "flex flex-col"}>
            {/* Register header (only in "All" tab) */}
            {showHeader && (
                <div className="flex items-center gap-2 px-4 py-1.5 bg-muted/40 border-b">
                    <h2 className="text-xs font-semibold">{registerName}</h2>
                    <Badge variant="secondary" className="text-[10px] h-4 px-1.5">
                        {items.length} зап.
                    </Badge>
                    {receipts > 0 && (
                        <Badge variant="secondary" className="text-[10px] h-4 px-1.5 bg-emerald-500/10 text-emerald-600">
                            <ArrowUpRight className="h-2.5 w-2.5 mr-0.5" />
                            {receipts}
                        </Badge>
                    )}
                    {expenses > 0 && (
                        <Badge variant="secondary" className="text-[10px] h-4 px-1.5 bg-rose-500/10 text-rose-600">
                            <ArrowDownRight className="h-2.5 w-2.5 mr-0.5" />
                            {expenses}
                        </Badge>
                    )}
                </div>
            )}

            {/* Table */}
            {fillHeight ? (
                <ScrollArea className="flex-1">
                    <MovementTable
                        sorted={sorted} columns={columns} decimalPlaces={decimalPlaces}
                        hasFooter={hasFooter} footerTotals={footerTotals}
                        handleSort={handleSort} sort={sort}
                        minR={minR} maxR={maxR} minC={minC} maxC={maxC}
                        onSelectionStart={onSelectionStart} onSelectionMove={onSelectionMove}
                    />
                    <ScrollBar orientation="horizontal" />
                </ScrollArea>
            ) : (
                <MovementTable
                    sorted={sorted} columns={columns} decimalPlaces={decimalPlaces}
                    hasFooter={hasFooter} footerTotals={footerTotals}
                    handleSort={handleSort} sort={sort}
                    minR={minR} maxR={maxR} minC={minC} maxC={maxC}
                    onSelectionStart={onSelectionStart} onSelectionMove={onSelectionMove}
                />
            )}

            {/* Status bar — Sum/Avg/Count */}
            <MovementStatusBar
                selectionRange={selectionRange}
                rows={sorted}
                columns={columns}
                decimalPlaces={decimalPlaces}
            />
        </div>
    )
}

// ── Movement Table (extracted to avoid duplication) ─────────────────────

interface MovementTableProps {
    sorted: DocumentMovement[]
    columns: MovementColumnDef[]
    decimalPlaces: number
    hasFooter: boolean
    footerTotals: Record<string, number>
    handleSort: (col: string) => void
    sort: SortState
    minR: number; maxR: number; minC: number; maxC: number
    onSelectionStart: (r: number, c: number) => void
    onSelectionMove: (r: number, c: number) => void
}

function MovementTable({
    sorted, columns, decimalPlaces, hasFooter, footerTotals,
    handleSort, sort, minR, maxR, minC, maxC,
    onSelectionStart, onSelectionMove,
}: MovementTableProps) {
    return (
        <table className="w-full text-xs select-none border-separate border-spacing-0">
            <thead className="sticky top-0 z-10 bg-card">
                <tr className="border-b bg-muted/60">
                    <th className="px-2 py-1.5 font-medium text-center w-[120px] text-muted-foreground whitespace-nowrap border-b">Период</th>
                    <th className="px-2 py-1.5 font-medium text-center w-8 text-muted-foreground border-b">Вид</th>
                    {columns.map(col => (
                        <th
                            key={col.key}
                            className={`px-2 py-1.5 font-medium text-muted-foreground cursor-pointer select-none hover:text-foreground transition-colors border-b ${
                                col.type === "amount" || col.type === "quantity" ? "text-right" : "text-left"
                            }`}
                            onClick={() => handleSort(col.key)}
                        >
                            <span className={`inline-flex items-center gap-0.5 ${
                                col.type === "amount" || col.type === "quantity" ? "justify-end w-full" : ""
                            }`}>
                                {col.label}
                                {sort.column !== col.key && <ArrowUpDown className="h-3 w-3 opacity-30 flex-shrink-0" />}
                                {sort.column === col.key && sort.dir === "asc" && <ArrowUp className="h-3 w-3 text-primary flex-shrink-0" />}
                                {sort.column === col.key && sort.dir === "desc" && <ArrowDown className="h-3 w-3 text-primary flex-shrink-0" />}
                            </span>
                        </th>
                    ))}
                </tr>
            </thead>
            <tbody>
                {sorted.map((m, rowIdx) => (
                    <tr key={rowIdx} className="hover:bg-muted/30 transition-colors cursor-cell border-b border-border/40">
                        <td className="px-2 py-1 text-center text-muted-foreground tabular-nums whitespace-nowrap text-[11px]">
                            {new Date(m.period).toLocaleString("ru-RU", {
                                day: "2-digit", month: "2-digit", year: "numeric",
                                hour: "2-digit", minute: "2-digit", second: "2-digit"
                            })}
                        </td>
                        <td className="px-2 py-1 text-center">
                            {m.recordType === "receipt" ? (
                                <span title="Приход">
                                    <ArrowUpRight className="h-3.5 w-3.5 text-emerald-500 mx-auto" />
                                </span>
                            ) : (
                                <span title="Расход">
                                    <ArrowDownRight className="h-3.5 w-3.5 text-rose-500 mx-auto" />
                                </span>
                            )}
                        </td>
                        {columns.map((col, colIdx) => {
                            const val = m.data[col.key]
                            const isSelected = rowIdx >= minR && rowIdx <= maxR && colIdx >= minC && colIdx <= maxC

                            return (
                                <td
                                    key={col.key}
                                    onMouseDown={(e) => { if (e.button === 0) onSelectionStart(rowIdx, colIdx) }}
                                    onMouseEnter={() => onSelectionMove(rowIdx, colIdx)}
                                    className={`px-2 py-1 ${
                                        col.type === "amount" || col.type === "quantity"
                                            ? "text-right font-mono tabular-nums"
                                            : "text-left"
                                    } ${
                                        isSelected ? "bg-primary/20 ring-1 ring-inset ring-primary/40" : ""
                                    }`}
                                >
                                    <CellValue val={val} col={col} decimalPlaces={decimalPlaces} />
                                </td>
                            )
                        })}
                    </tr>
                ))}

                {/* Footer totals */}
                {hasFooter && (
                    <tr className="bg-muted font-semibold border-t-2 border-border">
                        <td className="px-2 py-1.5 text-right text-xs" colSpan={2}>
                            ИТОГО
                        </td>
                        {columns.map(col => (
                            <td
                                key={col.key}
                                className={`px-2 py-1.5 text-xs ${
                                    col.type === "amount" || col.type === "quantity"
                                        ? "text-right font-mono tabular-nums"
                                        : ""
                                }`}
                            >
                                {col.type === "amount" && footerTotals[col.key] !== undefined
                                    ? fmtAmount(footerTotals[col.key], decimalPlaces)
                                    : col.type === "quantity" && footerTotals[col.key] !== undefined
                                        ? footerTotals[col.key].toLocaleString("ru-RU", { maximumFractionDigits: 3 })
                                        : ""
                                }
                            </td>
                        ))}
                    </tr>
                )}
            </tbody>
        </table>
    )
}

// ── Cell Value Renderer ─────────────────────────────────────────────────

function CellValue({ val, col, decimalPlaces }: {
    val: string | number | boolean | null | MovementRefValue | undefined
    col: MovementColumnDef
    decimalPlaces: number
}) {
    if (val === undefined || val === null) {
        return <span className="text-muted-foreground">—</span>
    }

    if (col.type === "ref") {
        const refVal = val as { id: string; name: string; url?: string }
        if (refVal.url) {
            return (
                <Link
                    href={refVal.url}
                    className="text-foreground hover:underline underline-offset-4 decoration-muted-foreground/40"
                >
                    {refVal.name || refVal.id}
                </Link>
            )
        }
        return <>{refVal.name || refVal.id}</>
    }

    if (col.type === "amount") {
        return <>{fmtAmount(Number(val) || 0, decimalPlaces)}</>
    }

    if (col.type === "quantity") {
        return <>{(Number(val) || 0).toLocaleString("ru-RU", { maximumFractionDigits: 3 })}</>
    }

    return <span title={String(val)}>{String(val)}</span>
}

// ── Status Bar (Sum / Avg / Count) ──────────────────────────────────────

function MovementStatusBar({
    selectionRange,
    rows,
    columns,
    decimalPlaces,
}: {
    selectionRange: SelectionRange | null
    rows: DocumentMovement[]
    columns: MovementColumnDef[]
    decimalPlaces: number
}) {
    if (!selectionRange) return null

    const minR = Math.min(selectionRange.start.r, selectionRange.end.r)
    const maxR = Math.max(selectionRange.start.r, selectionRange.end.r)
    const minC = Math.min(selectionRange.start.c, selectionRange.end.c)
    const maxC = Math.max(selectionRange.start.c, selectionRange.end.c)

    const selectedCols = columns.slice(minC, maxC + 1)
    const numericCols = selectedCols.filter(c => c.type === "amount" || c.type === "quantity")
    if (numericCols.length === 0) return null

    const selectedItems = rows.slice(minR, maxR + 1)
    if (selectedItems.length === 0) return null

    const aggregations = numericCols.map(col => {
        let sum = new Decimal(0)
        let count = 0
        for (const m of selectedItems) {
            const val = m.data[col.key]
            if (val !== undefined && val !== null && val !== "") {
                try {
                    sum = sum.plus(new Decimal(String(val)))
                    count++
                } catch { /* skip non-numeric */ }
            }
        }
        const avg = count > 0 ? sum.dividedBy(count) : new Decimal(0)
        return { col, sum, avg, count }
    }).filter(a => a.count > 0)

    if (aggregations.length === 0) return null

    // Format values based on column type
    const fmt = (val: Decimal, col: MovementColumnDef) => {
        if (col.type === "amount") return fmtAmount(val.toNumber(), decimalPlaces)
        return val.toNumber().toLocaleString("ru-RU", { maximumFractionDigits: 4 })
    }

    return (
        <div className="sticky bottom-0 bg-card border-t shadow-[0_-2px_4px_rgba(0,0,0,0.05)] z-20 flex flex-wrap gap-x-5 gap-y-1 items-center text-xs shrink-0 px-4 h-9">
            <span className="text-muted-foreground">
                Ячеек: {selectedItems.length * selectedCols.length}
            </span>
            {aggregations.map(({ col, sum, avg, count }) => (
                <div key={col.key} className="flex items-center gap-3">
                    <span className="text-muted-foreground">{col.label}:</span>
                    <span className="inline-flex items-center gap-1">
                        <span className="text-muted-foreground text-[10px] uppercase font-semibold">Sum</span>
                        <span className="font-medium tabular-nums">{fmt(sum, col)}</span>
                    </span>
                    {count > 1 && (
                        <span className="inline-flex items-center gap-1 border-l pl-2">
                            <span className="text-muted-foreground text-[10px] uppercase font-semibold">Avg</span>
                            <span className="tabular-nums opacity-90">{fmt(avg, col)}</span>
                        </span>
                    )}
                    <Badge variant="outline" className="text-[10px] font-normal h-4 px-1 border-dashed" title="Количество значений">
                        {count}
                    </Badge>
                </div>
            ))}
        </div>
    )
}

// ── Helpers ─────────────────────────────────────────────────────────────

/** Format cell value as plain text for clipboard TSV */
function formatCellRaw(
    val: string | number | boolean | null | MovementRefValue | undefined,
    col: MovementColumnDef,
    decimalPlaces: number,
): string {
    if (val === undefined || val === null) return ""
    if (col.type === "ref") {
        const r = val as MovementRefValue
        return r.name || r.id || ""
    }
    if (col.type === "amount") return fmtAmount(Number(val) || 0, decimalPlaces)
    if (col.type === "quantity") return (Number(val) || 0).toLocaleString("ru-RU", { maximumFractionDigits: 3 })
    return String(val)
}

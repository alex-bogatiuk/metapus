"use client"

/**
 * DocumentMovementsPage — full-page view of register movements for a document.
 *
 * Designed for accountants and technical specialists who need to audit
 * how a document affected registers. Key UX features:
 * - Sortable columns (click header to sort)
 * - Grouped by register name (like 1C "Движения документа")
 * - Full-width data table with monospaced numbers
 * - Back button returns to the document form
 *
 * Analogous to:
 * - 1C: "Движения документа" (Ctrl+F11)
 * - SAP: Material Document Display (MB03) → Accounting tab
 * - ERPNext: Stock Ledger → filtered by voucher
 */

import { useState, useEffect, useMemo, useCallback } from "react"
import Link from "next/link"
import { ArrowLeft, Loader2, ArrowUpRight, ArrowDownRight, ArrowUpDown, ArrowUp, ArrowDown } from "lucide-react"
import { Button } from "@/components/ui/button"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { Badge } from "@/components/ui/badge"
import { useCurrencyScale } from "@/hooks/useCurrencyScale"
import { fmtAmount } from "@/lib/format"
import type { DocumentMovementsResponse, DocumentMovement, MovementRefValue } from "@/types/common"

interface DocumentMovementsPageProps {
    /** Document identifier */
    documentId: string
    /** Back URL — typically the document form */
    backHref: string
    /** Page title (document number/name) */
    documentTitle: string
    /** API fetcher */
    fetcher: (id: string) => Promise<DocumentMovementsResponse>
}

type SortDir = "asc" | "desc" | null
interface SortState {
    column: string
    dir: SortDir
}

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
                .catch((e) => { if (isMounted) setError(e instanceof Error ? e.message : "Ошибка загрузки") })
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

            {/* Content */}
            {loading && (
                <div className="flex flex-1 items-center justify-center">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
            )}

            {error && (
                <div className="flex flex-1 items-center justify-center text-sm text-destructive">
                    {error}
                </div>
            )}

            {!loading && !error && movements.length === 0 && (
                <div className="flex flex-1 flex-col items-center justify-center text-muted-foreground">
                    <span className="text-sm">Документ не проведён или не содержит движений</span>
                </div>
            )}

            {!loading && !error && movements.length > 0 && (
                <ScrollArea className="flex-1">
                <div className="p-4 space-y-6">
                    {Object.entries(grouped).map(([registerName, items]) => (
                        <RegisterGroupTable key={registerName} registerName={registerName} items={items} />
                    ))}
                </div>
                </ScrollArea>
            )}
        </div>
    )
}

// ── Sortable register group table ───────────────────────────────────────

function RegisterGroupTable({ registerName, items }: { registerName: string, items: DocumentMovement[] }) {
    const [sort, setSort] = useState<SortState>({ column: "", dir: null })

    // Build unique columns array from the first item since all items in a register share the same structure
    const columns = useMemo(() => {
        if (!items.length) return []
        return items[0].columns || []
    }, [items])

    // Extract currencyId from the first item's 'currency' dimension (all items in a register share the same currency)
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

    return (
        <div>
            {/* Register header */}
            <div className="flex items-center gap-2 mb-2">
                <h2 className="text-sm font-semibold">{registerName}</h2>
                <Badge variant="secondary" className="text-[10px] h-5">
                    {items.length} зап.
                </Badge>
                {receipts > 0 && (
                    <Badge variant="secondary" className="text-[10px] h-5 bg-emerald-500/10 text-emerald-600">
                        <ArrowUpRight className="h-3 w-3 mr-0.5" />
                        {receipts} приход
                    </Badge>
                )}
                {expenses > 0 && (
                    <Badge variant="secondary" className="text-[10px] h-5 bg-rose-500/10 text-rose-600">
                        <ArrowDownRight className="h-3 w-3 mr-0.5" />
                        {expenses} расход
                    </Badge>
                )}
            </div>

            {/* Sortable data table */}
            <div className="border rounded-lg overflow-hidden bg-card">
                <ScrollArea className="w-full">
                    <table className="w-full text-xs">
                        <thead>
                            <tr className="border-b bg-muted/60">
                                <th className="px-3 py-2 font-medium text-center w-10 text-muted-foreground whitespace-nowrap">Период</th>
                                <th className="px-3 py-2 font-medium text-center w-10 text-muted-foreground">Вид</th>
                                {columns.map(col => (
                                    <th
                                        key={col.key}
                                        className={`px-3 py-2 font-medium text-muted-foreground cursor-pointer select-none hover:text-foreground transition-colors ${
                                            col.type === "amount" || col.type === "quantity" ? "text-right" : "text-left"
                                        }`}
                                        onClick={() => handleSort(col.key)}
                                    >
                                        <span className={`inline-flex items-center ${
                                            col.type === "amount" || col.type === "quantity" ? "justify-end w-full" : ""
                                        }`}>
                                            {col.label}
                                            {sort.column !== col.key && <ArrowUpDown className="h-3 w-3 opacity-30 ml-1 flex-shrink-0" />}
                                            {sort.column === col.key && sort.dir === "asc" && <ArrowUp className="h-3 w-3 text-primary ml-1 flex-shrink-0" />}
                                            {sort.column === col.key && sort.dir === "desc" && <ArrowDown className="h-3 w-3 text-primary ml-1 flex-shrink-0" />}
                                        </span>
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody className="divide-y">
                            {sorted.map((m, idx) => (
                                <tr key={idx} className="hover:bg-muted/30 transition-colors">
                                    <td className="px-3 py-1.5 text-center text-muted-foreground tabular-nums whitespace-nowrap text-[11px]">
                                        {new Date(m.period).toLocaleString("ru-RU", {
                                            day: "2-digit", month: "2-digit", year: "numeric",
                                            hour: "2-digit", minute: "2-digit", second: "2-digit"
                                        })}
                                    </td>
                                    <td className="px-3 py-1.5 text-center">
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
                                    {columns.map(col => {
                                        const val = m.data[col.key]

                                        if (val === undefined || val === null) {
                                            return <td key={col.key} className="px-3 py-1.5 text-muted-foreground">-</td>
                                        }

                                        if (col.type === "ref") {
                                            const refVal = val as { id: string, name: string, url?: string }
                                            if (refVal.url) {
                                                return (
                                                    <td key={col.key} className="px-3 py-1.5 text-left text-foreground hover:underline underline-offset-4 decoration-muted-foreground/40">
                                                        <Link href={refVal.url}>{refVal.name || refVal.id}</Link>
                                                    </td>
                                                )
                                            }
                                            return (
                                                <td key={col.key} className="px-3 py-1.5 text-left">
                                                    {refVal.name || refVal.id}
                                                </td>
                                            )
                                        }

                                        if (col.type === "amount") {
                                            return (
                                                <td key={col.key} className="px-3 py-1.5 text-right font-mono tabular-nums">
                                                    {fmtAmount(Number(val) || 0, decimalPlaces)}
                                                </td>
                                            )
                                        }

                                        if (col.type === "quantity") {
                                            const qtyStr = (Number(val) || 0).toLocaleString("ru-RU", {
                                                maximumFractionDigits: 3
                                            })
                                            return (
                                                <td key={col.key} className="px-3 py-1.5 text-right font-mono tabular-nums">
                                                    {qtyStr}
                                                </td>
                                            )
                                        }

                                        return (
                                            <td key={col.key} className="px-3 py-1.5 text-left" title={String(val)}>
                                                {String(val)}
                                            </td>
                                        )
                                    })}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                    <ScrollBar orientation="horizontal" />
                </ScrollArea>
            </div>
        </div>
    )
}

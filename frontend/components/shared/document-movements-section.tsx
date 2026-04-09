"use client"

import { Activity, Loader2, ArrowUpRight, ArrowDownRight } from "lucide-react"
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area"
import { useDocumentMovements } from "@/hooks/useDocumentMovements"
import type { DocumentMovementsResponse, DocumentMovement } from "@/types/common"

interface DocumentMovementsSectionProps {
    /** Document ID to fetch movements for */
    documentId: string
    /** API fetch function */
    fetcher: (id: string) => Promise<DocumentMovementsResponse>
    /** When false, no fetch is initiated (lazy loading). */
    enabled: boolean
}

export function DocumentMovementsSection({ documentId, fetcher, enabled }: DocumentMovementsSectionProps) {
    const { movements, loading, error } = useDocumentMovements({
        fetcher,
        documentId,
        enabled,
    })

    if (!enabled) return null

    if (loading) {
        return (
            <div>
                <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground mb-3">
                    <Activity className="h-4 w-4" />
                    Движения документа
                </div>
                <div className="flex items-center justify-center py-4">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                </div>
            </div>
        )
    }

    if (error) {
        return (
            <div>
                <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground mb-3">
                    <Activity className="h-4 w-4" />
                    Движения документа
                </div>
                <div className="text-xs text-destructive/80 py-2">{error}</div>
            </div>
        )
    }

    if (movements.length === 0) return null

    // Group movements by registerName
    const grouped = movements.reduce<Record<string, DocumentMovement[]>>((acc, m) => {
        if (!acc[m.registerName]) acc[m.registerName] = []
        acc[m.registerName].push(m)
        return acc
    }, {})

    return (
        <div>
            <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground mb-3">
                <Activity className="h-4 w-4" />
                Движения документа
            </div>
            <div className="space-y-4">
                {Object.entries(grouped).map(([registerName, items]) => (
                    <MovementGroupView key={registerName} registerName={registerName} items={items} />
                ))}
            </div>
        </div>
    )
}

function MovementGroupView({ registerName, items }: { registerName: string, items: DocumentMovement[] }) {
    // Collect all unique keys from `data` across all items in this group
    const columns = Array.from(new Set(items.flatMap(item => Object.keys(item.data))))

    return (
        <div className="space-y-1.5">
            <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground/80 uppercase tracking-wide">
                <span className="truncate">{registerName}</span>
                <span className="ml-auto text-[10px] tabular-nums bg-foreground/10 rounded px-1.5 py-0.5">
                    {items.length}
                </span>
            </div>
            <div className="border rounded-md overflow-hidden bg-background">
                <ScrollArea className="w-full">
                    <table className="w-full text-xs">
                        <thead>
                            <tr className="border-b bg-muted/50 text-muted-foreground">
                                <th className="px-2 py-1.5 font-medium border-r text-center w-8">#</th>
                                <th className="px-2 py-1.5 font-medium border-r text-center w-8">Вид</th>
                                {columns.map(col => (
                                    <th key={col} className="px-2 py-1.5 font-medium text-left border-r last:border-r-0">
                                        {col}
                                    </th>
                                ))}
                            </tr>
                        </thead>
                        <tbody className="divide-y">
                            {items.map((m, idx) => (
                                <tr key={idx} className="hover:bg-muted/50">
                                    <td className="px-2 py-1 border-r text-center text-muted-foreground">{idx + 1}</td>
                                    <td className="px-2 py-1 border-r text-center">
                                        {m.recordType === "receipt" ? (
                                            <span title="Приход"><ArrowUpRight className="h-3 w-3 text-emerald-500 mx-auto" /></span>
                                        ) : (
                                            <span title="Расход"><ArrowDownRight className="h-3 w-3 text-rose-500 mx-auto" /></span>
                                        )}
                                    </td>
                                    {columns.map(col => (
                                        <td key={col} className="px-2 py-1 border-r last:border-r-0 text-foreground/80 truncate max-w-[120px]" title={String(m.data[col] ?? "")}>
                                            {m.data[col] !== undefined && m.data[col] !== null ? String(m.data[col]) : ""}
                                        </td>
                                    ))}
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

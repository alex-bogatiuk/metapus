"use client"

/**
 * RelatedDocumentsTab — full-width tab component showing related documents.
 *
 * Renders a table in the style of 1C's "Структура подчинённости":
 *   Документ | Сумма | Контрагент
 *
 * Each document is a clickable link navigating to its form.
 * Documents are grouped by type (e.g. "Реализации товаров", "Поступления товаров").
 *
 * Usage:
 *   <TabsContent value="related">
 *     <RelatedDocumentsTab documentId={id} fetcher={fn} enabled={true} />
 *   </TabsContent>
 */

import Link from "next/link"
import { FileText, Loader2, ChevronRight } from "lucide-react"
import { useRelatedDocuments } from "@/hooks/useRelatedDocuments"
import { ScrollArea } from "@/components/ui/scroll-area"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import type { RelatedDocGroup, RelatedDocumentsResponse } from "@/types/common"

interface RelatedDocumentsTabProps {
    /** Document ID to fetch related documents for */
    documentId: string
    /** API fetch function */
    fetcher: (id: string) => Promise<RelatedDocumentsResponse>
    /** When false, no fetch is initiated (lazy loading). */
    enabled: boolean
}

export function RelatedDocumentsTab({ documentId, fetcher, enabled }: RelatedDocumentsTabProps) {
    const { groups, loading, error } = useRelatedDocuments({
        fetcher,
        documentId,
        enabled,
    })

    if (!enabled) return null

    if (loading) {
        return (
            <div className="flex items-center justify-center py-12">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
        )
    }

    if (error) {
        return (
            <div className="flex items-center justify-center py-12 text-sm text-destructive/80">
                {error}
            </div>
        )
    }

    if (groups.length === 0) {
        return (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
                <FileText className="h-8 w-8 mb-2 opacity-40" />
                <span className="text-sm">Связанные документы не найдены</span>
            </div>
        )
    }

    // Flatten all items from all groups into a single table
    // with group headers as separator rows (like 1C tree structure)
    return (
        <ScrollArea className="h-full">
            <table className="w-full text-sm border-separate border-spacing-0">
                <thead className="sticky top-0 z-10 bg-muted/90 backdrop-blur-sm">
                    <tr>
                        <th className="border-b px-3 py-2 text-left text-[11px] font-semibold text-muted-foreground w-12">
                            №
                        </th>
                        <th className="border-b px-3 py-2 text-left text-[11px] font-semibold text-muted-foreground">
                            Документ
                        </th>
                        <th className="border-b px-3 py-2 text-left text-[11px] font-semibold text-muted-foreground w-[180px]">
                            Тип
                        </th>
                    </tr>
                </thead>
                <tbody>
                    {groups.map((group) => (
                        <RelatedGroupRows key={group.entityName} group={group} />
                    ))}
                </tbody>
            </table>
        </ScrollArea>
    )
}

// ── Internal: group rows ────────────────────────────────────────────────

function RelatedGroupRows({ group }: { group: RelatedDocGroup }) {
    const entityType = group.entityType === "document" ? "document" : "catalog"
    const hasMore = group.totalCount > group.items.length

    return (
        <>
            {/* Group header row */}
            <tr className="bg-muted/40">
                <td
                    colSpan={3}
                    className="px-3 py-1.5 text-[11px] font-semibold text-muted-foreground uppercase tracking-wide border-b"
                >
                    <div className="flex items-center gap-1.5">
                        <FileText className="h-3.5 w-3.5 shrink-0" />
                        <span>{group.presentation}</span>
                        <span className="ml-1 text-[10px] tabular-nums bg-foreground/10 rounded px-1.5 py-0.5 normal-case">
                            {group.totalCount}
                        </span>
                    </div>
                </td>
            </tr>

            {/* Document items */}
            {group.items.map((item, idx) => {
                const url = buildEntityUrlByRoute(group.routePrefix, entityType, item.id)
                return (
                    <tr key={item.id} className="hover:bg-accent/50 transition-colors group">
                        <td className="px-3 py-1.5 border-b text-muted-foreground text-center tabular-nums text-xs">
                            {idx + 1}
                        </td>
                        <td className="px-3 py-1.5 border-b">
                            <Link
                                href={url}
                                className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline underline-offset-2"
                            >
                                <span>{item.presentation}</span>
                                <ChevronRight className="h-3 w-3 shrink-0 opacity-0 group-hover:opacity-50 transition-opacity" />
                            </Link>
                        </td>
                        <td className="px-3 py-1.5 border-b text-xs text-muted-foreground">
                            {group.presentation}
                        </td>
                    </tr>
                )
            })}

            {/* "Show more" row */}
            {hasMore && (
                <tr>
                    <td className="border-b" />
                    <td colSpan={2} className="px-3 py-1 border-b">
                        <span className="text-[11px] text-muted-foreground italic">
                            и ещё {group.totalCount - group.items.length}…
                        </span>
                    </td>
                </tr>
            )}
        </>
    )
}

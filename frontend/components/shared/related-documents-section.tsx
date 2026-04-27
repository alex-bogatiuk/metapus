"use client"

/**
 * RelatedDocumentsSection — sidebar component showing related documents.
 *
 * Renders groups of documents that reference the current document.
 * Each item is a clickable link navigating to the related document form.
 *
 * Design: compact, metadata-driven (like 1C "Структура подчиненности" / Odoo "Smart Buttons").
 *
 * Usage:
 *   <FormSidebar collapsed={collapsed} onToggle={toggle} meta={meta}>
 *       <RelatedDocumentsSection
 *           documentId={params.id}
 *           fetcher={(id) => api.goodsReceipts.getRelatedDocuments(id)}
 *           enabled={!collapsed}
 *       />
 *   </FormSidebar>
 */

import Link from "next/link"
import { FileText, ChevronRight, Loader2, LinkIcon } from "lucide-react"
import { useRelatedDocuments } from "@/hooks/useRelatedDocuments"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import type { RelatedDocGroup, RelatedDocumentsResponse } from "@/types/common"

interface RelatedDocumentsSectionProps {
    /** Document ID to fetch related documents for */
    documentId: string
    /** API fetch function */
    fetcher: (id: string) => Promise<RelatedDocumentsResponse>
    /** When false, no fetch is initiated (lazy loading). Typically: !sidebarCollapsed */
    enabled: boolean
}

export function RelatedDocumentsSection({ documentId, fetcher, enabled }: RelatedDocumentsSectionProps) {
    const { groups, loading, error } = useRelatedDocuments({
        fetcher,
        documentId,
        enabled,
    })

    // Don't render anything until enabled (sidebar expanded)
    if (!enabled) return null

    // Loading state
    if (loading) {
        return (
            <div>
                <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground mb-3">
                    <LinkIcon className="h-4 w-4" />
                    Связанные документы
                </div>
                <div className="flex items-center justify-center py-4">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                </div>
            </div>
        )
    }

    // Error state
    if (error) {
        return (
            <div>
                <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground mb-3">
                    <LinkIcon className="h-4 w-4" />
                    Связанные документы
                </div>
                <div className="text-xs text-destructive/80 py-2">{error}</div>
            </div>
        )
    }

    // No related documents found — hide section entirely
    if (groups.length === 0) return null

    return (
        <div>
            <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground mb-3">
                <LinkIcon className="h-4 w-4" />
                Связанные документы
            </div>
            <div className="space-y-3">
                {groups.map((group) => (
                    <RelatedDocGroupView key={group.entityName} group={group} />
                ))}
            </div>
        </div>
    )
}

// ── Internal: group view ────────────────────────────────────────────────

function RelatedDocGroupView({ group }: { group: RelatedDocGroup }) {
    const entityType = group.entityType === "document" ? "document" : "catalog"
    const hasMore = group.totalCount > group.items.length

    return (
        <div>
            <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground/80 uppercase tracking-wide mb-1.5">
                <FileText className="h-3 w-3 shrink-0" />
                <span className="truncate">{group.presentation}</span>
                <span className="ml-auto text-[10px] tabular-nums bg-foreground/10 rounded px-1.5 py-0.5">
                    {group.totalCount}
                </span>
            </div>
            <div className="space-y-0.5">
                {group.items.map((item) => {
                    const url = buildEntityUrlByRoute(group.routePrefix, entityType, item.id)
                    return (
                        <Link
                            key={item.id}
                            href={url}
                            className="flex items-center gap-1.5 px-2 py-1 rounded text-xs text-foreground/80 hover:bg-accent hover:text-foreground transition-colors group"
                        >
                            <span className="truncate flex-1">{item.presentation}</span>
                            <ChevronRight className="h-3 w-3 shrink-0 opacity-0 group-hover:opacity-50 transition-opacity" />
                        </Link>
                    )
                })}
                {hasMore && (
                    <Link
                        href={`/admin/find-references`}
                        className="flex items-center px-2 py-1 text-[11px] text-primary/70 hover:text-primary transition-colors"
                    >
                        и ещё {group.totalCount - group.items.length}…
                    </Link>
                )}
            </div>
        </div>
    )
}

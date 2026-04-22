"use client"

import Link from "next/link"
import { useRouter } from "next/navigation"
import { CircleCheck, Circle } from "lucide-react"
import { useWidgetData } from "@/hooks/useWidgetData"
import { api } from "@/lib/api"
import { useMetadataStore } from "@/stores/useMetadataStore"
import { buildEntityUrl } from "@/lib/entity-url"
import type { WidgetRenderProps } from "@/types/dashboard"
import { ScrollArea } from "@/components/ui/scroll-area"
import type { DocumentJournalItem } from "@/types/reports"
import { fmtDate, fmtAmount } from "@/lib/format"

export default function RecentDocsRenderer({ config, isEditMode }: WidgetRenderProps<"recent-documents">) {
    const limit = config.limit ?? 5
    const documentTypes = config.documentTypes ?? []

    const { data, loading, error } = useWidgetData(
        async () => {
            const journal = await api.reports.getDocumentJournal({
                limit,
                posted: undefined,
                documentType: documentTypes.length > 0 ? documentTypes : undefined,
            })
            return journal.items
        },
        { deps: [limit, documentTypes.join(",")], isEditMode, pollInterval: 60_000 }
    )

    const router = useRouter()
    const items: DocumentJournalItem[] = data ?? []

    return (
        <div className="flex h-full flex-col">
            <div className="flex items-center justify-between border-b px-4 py-3">
                <h3 className="text-sm font-semibold text-foreground">
                    Последние документы
                </h3>
                <Link
                    href="/documents/goods-receipts"
                    className="text-xs font-medium text-foreground hover:underline"
                >
                    Все документы
                </Link>
            </div>
            <ScrollArea className="flex-1">
                {error ? (
                    <div className="flex h-full flex-col items-center justify-center gap-1 p-4 text-center">
                        <p className="text-sm text-destructive">Ошибка загрузки</p>
                        <p className="text-xs text-muted-foreground line-clamp-2">{error.message}</p>
                    </div>
                ) : loading && items.length === 0 ? (
                    <div className="space-y-2 p-4">
                        {Array.from({ length: limit }).map((_, i) => (
                            <div key={i} className="h-8 animate-pulse rounded bg-muted" />
                        ))}
                    </div>
                ) : items.length === 0 ? (
                    <div className="flex h-full items-center justify-center p-4">
                        <p className="text-sm text-muted-foreground">Нет документов</p>
                    </div>
                ) : (
                    <table className="w-full text-sm">
                        <thead>
                            <tr className="border-b bg-muted/50">
                                <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">Тип</th>
                                <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">Номер</th>
                                <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">Дата</th>
                                <th className="px-4 py-2 text-left text-xs font-medium text-muted-foreground">Контрагент</th>
                                <th className="px-4 py-2 text-right text-xs font-medium text-muted-foreground">Сумма</th>
                            </tr>
                        </thead>
                        <tbody>
                            {items.map((doc) => (
                                <tr
                                    key={doc.id}
                                    className="border-b last:border-0 hover:bg-muted/30 transition-colors cursor-pointer"
                                    onClick={() => router.push(docHref(doc))}
                                >
                                    <td className="px-4 py-2.5 text-foreground flex items-center gap-2">
                                        {doc.posted ? (
                                            <CircleCheck className="h-4 w-4 text-success" />
                                        ) : (
                                            <Circle className="h-4 w-4 text-muted-foreground" />
                                        )}
                                        <span className="truncate">{formatDocType(doc.documentType)}</span>
                                    </td>
                                    <td className="px-4 py-2.5 font-mono text-xs text-foreground">{doc.number}</td>
                                    <td className="px-4 py-2.5 text-muted-foreground">{fmtDate(doc.date)}</td>
                                    <td className="px-4 py-2.5 text-foreground truncate max-w-[200px]">{doc.counterpartyName || "—"}</td>
                                    <td className="px-4 py-2.5 text-right font-mono text-foreground">
                                        {fmtAmount(doc.totalAmount || 0)} {doc.currency}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                )}
            </ScrollArea>
        </div>
    )
}

/**
 * Resolve document type label and URL from metadata.
 * Falls back to entityKey if metadata is not loaded.
 */
function formatDocType(type: string): string {
    const meta = useMetadataStore.getState()
    const label = meta.getLabel(type, "singular")
    return label !== type ? label : type
}

function docHref(doc: DocumentJournalItem): string {
    const url = buildEntityUrl(doc.documentType, doc.id)
    return url ?? "#"
}

"use client"

import { useWidgetData } from "@/hooks/useWidgetData"
import { api } from "@/lib/api"
import type { WidgetRenderProps } from "@/types/dashboard"
import { ScrollArea } from "@/components/ui/scroll-area"

export default function EventLogRenderer({ config, isEditMode }: WidgetRenderProps<"event-log">) {
    const limit = config.limit ?? 5

    const { data, loading } = useWidgetData(
        async () => {
            const result = await api.system.eventLog.list({ limit: String(limit) })
            return result.items
        },
        { deps: [limit], isEditMode, pollInterval: 30_000 }
    )

    const items = data ?? []

    return (
        <div className="flex h-full flex-col">
            <div className="border-b px-4 py-3">
                <h3 className="text-sm font-semibold text-foreground">Журнал событий</h3>
            </div>
            <ScrollArea className="flex-1">
                {loading && items.length === 0 ? (
                    <div className="space-y-2 p-4">
                        {Array.from({ length: limit }).map((_, i) => (
                            <div key={i} className="h-6 animate-pulse rounded bg-muted" />
                        ))}
                    </div>
                ) : items.length === 0 ? (
                    <div className="flex h-full items-center justify-center p-4">
                        <p className="text-sm text-muted-foreground">Нет событий</p>
                    </div>
                ) : (
                    <div className="divide-y">
                        {items.map((entry) => (
                            <div key={entry.id} className="px-4 py-2.5 hover:bg-muted/30 transition-colors">
                                <div className="flex items-center justify-between gap-2">
                                    <span className="text-sm font-medium text-foreground truncate">
                                        {entry.eventType}
                                    </span>
                                    <span className="text-xs text-muted-foreground shrink-0">
                                        {formatRelativeTime(entry.createdAt)}
                                    </span>
                                </div>
                                <p className="mt-0.5 text-xs text-muted-foreground truncate">
                                    {entry.entityType} {entry.entityId ? `#${entry.entityId.slice(0, 8)}` : ""}
                                    {entry.userEmail ? ` — ${entry.userEmail}` : ""}
                                </p>
                            </div>
                        ))}
                    </div>
                )}
            </ScrollArea>
        </div>
    )
}

function formatRelativeTime(iso: string): string {
    try {
        const date = new Date(iso)
        const now = new Date()
        const diffMs = now.getTime() - date.getTime()
        const diffMin = Math.floor(diffMs / 60000)
        if (diffMin < 1) return "только что"
        if (diffMin < 60) return `${diffMin} мин.`
        const diffHours = Math.floor(diffMin / 60)
        if (diffHours < 24) return `${diffHours} ч.`
        return date.toLocaleDateString("ru-RU", { day: "2-digit", month: "2-digit" })
    } catch {
        return iso
    }
}

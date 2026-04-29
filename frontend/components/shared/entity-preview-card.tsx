"use client"

/**
 * EntityPreviewCard — universal preview card for any entity (catalog or document).
 *
 * Used in Command Palette as a side panel when user presses → on a search result.
 * Fetches data lazily from GET /api/v1/search/preview.
 *
 * Features:
 * - Skeleton loading state
 * - Title + subtitle
 * - Dynamic label:value fields from metadata (INN, phone, email, etc.)
 * - Reference fields (resolved FK names like "Контрагент: ООО Ромашка")
 * - "Открыть" button at the bottom
 */

import * as React from "react"
import { Loader2, ExternalLink } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { api } from "@/lib/api"
import { getEntityIcon } from "@/lib/entity-icon"
import { cn } from "@/lib/utils"
import type { PreviewResponse } from "@/types/search"

// ── Props ───────────────────────────────────────────────────────────────

interface EntityPreviewCardProps {
    entityType: string   // "catalog" | "document"
    entityKey: string    // "counterparty" | "goods_receipt"
    entityId: string
    onNavigate: (url: string, title: string) => void
}

// ── Component ───────────────────────────────────────────────────────────

export function EntityPreviewCard({
    entityType,
    entityKey,
    entityId,
    onNavigate,
}: EntityPreviewCardProps) {
    const [data, setData] = React.useState<PreviewResponse | null>(null)
    const [loading, setLoading] = React.useState(true)
    const [error, setError] = React.useState<string | null>(null)

    React.useEffect(() => {
        setLoading(true)
        setError(null)
        setData(null)

        const controller = new AbortController()

        api.search
            .preview(entityType, entityKey, entityId)
            .then((res) => {
                if (!controller.signal.aborted) {
                    setData(res)
                    setLoading(false)
                }
            })
            .catch((err) => {
                if (!controller.signal.aborted) {
                    setError(err instanceof Error ? err.message : "Не удалось загрузить")
                    setLoading(false)
                }
            })

        return () => controller.abort()
    }, [entityType, entityKey, entityId])

    // ── Loading skeleton ────────────────────────────────────────────────

    if (loading) {
        return (
            <div className="flex flex-col items-center justify-center h-full gap-2 text-muted-foreground">
                <Loader2 className="h-5 w-5 animate-spin" />
                <span className="text-xs">Загрузка...</span>
            </div>
        )
    }

    // ── Error state ─────────────────────────────────────────────────────

    if (error || !data) {
        return (
            <div className="flex flex-col items-center justify-center h-full gap-2 text-muted-foreground">
                <span className="text-xs">{error ?? "Данные не найдены"}</span>
            </div>
        )
    }

    // ── Resolved data ───────────────────────────────────────────────────

    const IconComponent = getEntityIcon(data.entityKey)

    // Find status field if present
    const statusField = data.fields.find((f) => f.label === "Статус")
    const statusVariant = statusField
        ? statusField.value === "Проведён"
            ? "default"
            : statusField.value === "Удалён"
                ? "destructive"
                : "secondary"
        : undefined

    return (
        <div className="flex flex-col h-full">
            {/* ── Header ─────────────────────────────────────────── */}
            <div className="px-4 pt-4 pb-3 space-y-2">
                <div className="flex items-start gap-2">
                    <IconComponent className="h-5 w-5 text-muted-foreground mt-0.5 shrink-0" />
                    <div className="min-w-0">
                        <h3 className="text-sm font-semibold truncate">{data.title}</h3>
                        {data.subtitle && (
                            <p className="text-xs text-muted-foreground truncate">{data.subtitle}</p>
                        )}
                    </div>
                    {statusField && (
                        <Badge
                            variant={statusVariant}
                            className={cn(
                                "shrink-0 ml-auto text-[10px]",
                                statusField.value === "Проведён" && "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300",
                                statusField.value === "Черновик" && "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-300",
                            )}
                        >
                            {statusField.value}
                        </Badge>
                    )}
                </div>
            </div>

            <Separator />

            {/* ── Fields ─────────────────────────────────────────── */}
            <div className="flex-1 overflow-y-auto px-4 py-3 space-y-3">
                {/* Regular fields (skip "Статус" — shown in header badge) */}
                {data.fields.filter((f) => f.label !== "Статус").length > 0 && (
                    <div className="space-y-1.5">
                        {data.fields
                            .filter((f) => f.label !== "Статус")
                            .map((field) => (
                                <div key={field.label} className="flex items-baseline gap-2 text-xs">
                                    <span className="text-muted-foreground shrink-0 min-w-[60px]">
                                        {field.label}
                                    </span>
                                    <span className="font-medium truncate">{field.value}</span>
                                </div>
                            ))}
                    </div>
                )}

                {/* References (FK-resolved names) */}
                {data.references && Object.keys(data.references).length > 0 && (
                    <>
                        <Separator />
                        <div className="space-y-1.5">
                            {Object.entries(data.references).map(([label, value]) => (
                                <div key={label} className="flex items-baseline gap-2 text-xs">
                                    <span className="text-muted-foreground shrink-0 min-w-[60px]">
                                        {label}
                                    </span>
                                    <span className="font-medium truncate">{value}</span>
                                </div>
                            ))}
                        </div>
                    </>
                )}
            </div>

            {/* ── Footer: Open button ────────────────────────────── */}
            <div className="px-4 py-3 border-t">
                <Button
                    variant="outline"
                    size="sm"
                    className="w-full text-xs"
                    onClick={() => onNavigate(data.url, data.title)}
                >
                    <ExternalLink className="mr-1.5 h-3 w-3" />
                    Открыть
                </Button>
            </div>
        </div>
    )
}

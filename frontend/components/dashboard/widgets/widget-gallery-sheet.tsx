"use client"

import { useState, useCallback } from "react"
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "@/components/ui/sheet"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { allWidgetDefinitions } from "@/lib/widget-registry"
import { useDashboardStore } from "@/stores/useDashboardStore"
import type { WidgetCategory, WidgetDefinition, WidgetSize } from "@/types/dashboard"

const CATEGORY_LABELS: Record<WidgetCategory, string> = {
    kpi: "Показатели",
    lists: "Списки",
    charts: "Графики",
    actions: "Действия",
    system: "Система",
}

const SIZE_TO_WH: Record<WidgetSize, { w: number; h: number }> = {
    "2x1": { w: 3, h: 1 },
    "3x1": { w: 4, h: 1 },
    "4x1": { w: 6, h: 1 },
    "2x2": { w: 4, h: 2 },
    "3x2": { w: 6, h: 2 },
    "4x2": { w: 8, h: 2 },
    "4x3": { w: 8, h: 3 },
    "4x4": { w: 12, h: 4 },
}

interface WidgetGallerySheetProps {
    open: boolean
    onOpenChange: (open: boolean) => void
}

export function WidgetGallerySheet({ open, onOpenChange }: WidgetGallerySheetProps) {
    const addWidget = useDashboardStore((s) => s.addWidget)
    const widgets = useDashboardStore((s) => s.layout.widgets)
    const [filter, setFilter] = useState<WidgetCategory | "all">("all")

    const filtered = filter === "all"
        ? allWidgetDefinitions
        : allWidgetDefinitions.filter((d) => d.category === filter)

    const grouped = filtered.reduce<Record<string, WidgetDefinition[]>>((acc, def) => {
        const cat = CATEGORY_LABELS[def.category]
        ;(acc[cat] ??= []).push(def)
        return acc
    }, {})

    const handleAdd = useCallback(
        (def: WidgetDefinition) => {
            const size = SIZE_TO_WH[def.defaultSize]
            addWidget({
                instanceId: `${def.type}-${crypto.randomUUID().slice(0, 8)}`,
                widgetType: def.type,
                x: 0,
                y: Infinity,
                w: size.w,
                h: size.h,
                config: { ...def.defaultConfig },
            })
            onOpenChange(false)
        },
        [addWidget, onOpenChange]
    )

    const existingCount = (type: string) => widgets.filter((w) => w.widgetType === type).length

    return (
        <Sheet open={open} onOpenChange={onOpenChange}>
            <SheetContent side="right" className="w-[400px] sm:w-[440px]">
                <SheetHeader>
                    <SheetTitle>Добавить виджет</SheetTitle>
                    <SheetDescription>Выберите виджет для размещения на дашборде</SheetDescription>
                </SheetHeader>

                <div className="mt-4 flex flex-wrap gap-1.5">
                    <Button
                        variant={filter === "all" ? "default" : "outline"}
                        size="sm"
                        onClick={() => setFilter("all")}
                    >
                        Все
                    </Button>
                    {(Object.entries(CATEGORY_LABELS) as [WidgetCategory, string][]).map(([key, label]) => (
                        <Button
                            key={key}
                            variant={filter === key ? "default" : "outline"}
                            size="sm"
                            onClick={() => setFilter(key)}
                        >
                            {label}
                        </Button>
                    ))}
                </div>

                <ScrollArea className="mt-4 pb-8" style={{ maxHeight: "calc(100vh - 220px)" }}>
                <div className="space-y-4">
                    {Object.entries(grouped).map(([category, defs]) => (
                        <div key={category}>
                            <p className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                                {category}
                            </p>
                            <div className="space-y-2">
                                {defs.map((def) => {
                                    const Icon = def.icon
                                    const count = existingCount(def.type)
                                    return (
                                        <div
                                            key={def.type}
                                            className="flex items-start gap-3 rounded-lg border p-3 transition-colors hover:bg-muted/50"
                                        >
                                            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted">
                                                <Icon className="h-4.5 w-4.5 text-foreground" />
                                            </div>
                                            <div className="min-w-0 flex-1">
                                                <div className="flex items-center gap-2">
                                                    <p className="text-sm font-medium text-foreground">{def.label}</p>
                                                    {count > 0 && (
                                                        <Badge variant="secondary" className="text-[10px] px-1.5 py-0">
                                                            {count}
                                                        </Badge>
                                                    )}
                                                </div>
                                                <p className="mt-0.5 text-xs text-muted-foreground line-clamp-2">
                                                    {def.description}
                                                </p>
                                            </div>
                                            <Button
                                                variant="outline"
                                                size="sm"
                                                className="shrink-0"
                                                onClick={() => handleAdd(def)}
                                            >
                                                Добавить
                                            </Button>
                                        </div>
                                    )
                                })}
                            </div>
                        </div>
                    ))}
                </div>
                </ScrollArea>
            </SheetContent>
        </Sheet>
    )
}

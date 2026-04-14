"use client"

import { useState, useCallback, useMemo } from "react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Plus, Eye } from "lucide-react"
import { cn } from "@/lib/utils"
import { allWidgetDefinitions } from "@/lib/widget-registry"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { useIsMobile } from "@/hooks/use-mobile"
import { WidgetPreviewContainer } from "./widget-preview-container"
import { WidgetConfigPanel } from "./widget-config-panel"
import { toast } from "sonner"
import type {
    WidgetCategory,
    WidgetDefinition,
    WidgetSize,
    WidgetType,
} from "@/types/dashboard"
import { CATEGORY_LABELS, SIZE_TO_WH, SIZE_LABELS } from "@/types/dashboard"

// ── Constants ───────────────────────────────────────────────────────

const MAX_WIDGETS = 20

// ── Props ───────────────────────────────────────────────────────────

interface WidgetGalleryDialogProps {
    open: boolean
    onOpenChange: (open: boolean) => void
}

// ── Component ───────────────────────────────────────────────────────

export function WidgetGalleryDialog({ open, onOpenChange }: WidgetGalleryDialogProps) {
    const addWidget = useDashboardStore((s) => s.addWidget)
    const widgets = useDashboardStore((s) => s.layout.widgets)
    const isMobile = useIsMobile()

    const [filter, setFilter] = useState<WidgetCategory | "all">("all")
    const [selectedType, setSelectedType] = useState<WidgetType | null>(null)
    const [selectedSize, setSelectedSize] = useState<WidgetSize | null>(null)
    const [previewConfig, setPreviewConfig] = useState<Record<string, unknown>>({})
    // Mobile: toggle between catalog and preview tabs
    const [mobileTab, setMobileTab] = useState<"catalog" | "preview">("catalog")

    // ── Derived state ───────────────────────────────────────────────

    const filtered = filter === "all"
        ? allWidgetDefinitions
        : allWidgetDefinitions.filter((d) => d.category === filter)

    const grouped = useMemo(() =>
        filtered.reduce<Record<string, WidgetDefinition[]>>((acc, def) => {
            const cat = CATEGORY_LABELS[def.category]
            ;(acc[cat] ??= []).push(def)
            return acc
        }, {}),
        [filtered]
    )

    const selectedDef = useMemo(
        () => allWidgetDefinitions.find((d) => d.type === selectedType) ?? null,
        [selectedType]
    )

    const existingCount = useCallback(
        (type: string) => widgets.filter((w) => w.widgetType === type).length,
        [widgets]
    )

    const isLimitReached = widgets.length >= MAX_WIDGETS

    // ── Handlers ────────────────────────────────────────────────────

    const handleSelectWidget = useCallback((def: WidgetDefinition) => {
        setSelectedType(def.type)
        setSelectedSize(def.defaultSize)
        setPreviewConfig({ ...def.defaultConfig })
        if (isMobile) setMobileTab("preview")
    }, [isMobile])

    const handleAdd = useCallback(() => {
        if (!selectedDef || !selectedSize) return
        if (isLimitReached) return

        const size = SIZE_TO_WH[selectedSize]
        addWidget({
            instanceId: `${selectedDef.type}-${crypto.randomUUID().slice(0, 8)}`,
            widgetType: selectedDef.type,
            x: 0,
            y: Infinity,
            w: size.w,
            h: size.h,
            config: { ...previewConfig },
        })
        toast.success("Виджет добавлен на дашборд")
    }, [selectedDef, selectedSize, previewConfig, addWidget, isLimitReached])

    const handleConfigChange = useCallback((config: Record<string, unknown>) => {
        setPreviewConfig(config)
    }, [])

    // Auto-select first widget when dialog opens with nothing selected
    const handleOpenChange = useCallback((nextOpen: boolean) => {
        if (nextOpen && !selectedType && allWidgetDefinitions.length > 0) {
            const first = allWidgetDefinitions[0]
            setSelectedType(first.type)
            setSelectedSize(first.defaultSize)
            setPreviewConfig({ ...first.defaultConfig })
        }
        if (nextOpen) {
            setMobileTab("catalog")
        }
        onOpenChange(nextOpen)
    }, [selectedType, onOpenChange])

    // ── Catalog pane (shared between desktop and mobile) ────────────

    const catalogContent = (
        <>
            {/* Category filter buttons */}
            <div className="flex flex-wrap gap-1.5 px-4 py-3 border-b">
                <Button
                    variant={filter === "all" ? "default" : "outline"}
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => setFilter("all")}
                >
                    Все
                </Button>
                {(Object.entries(CATEGORY_LABELS) as [WidgetCategory, string][]).map(([key, label]) => (
                    <Button
                        key={key}
                        variant={filter === key ? "default" : "outline"}
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => setFilter(key)}
                    >
                        {label}
                    </Button>
                ))}
            </div>

            {/* Widget cards */}
            <ScrollArea className="flex-1">
                <div className="space-y-3 p-3">
                    {Object.entries(grouped).map(([category, defs]) => (
                        <div key={category}>
                            <p className="mb-1.5 px-1 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
                                {category}
                            </p>
                            <div className="space-y-1">
                                {defs.map((def) => {
                                    const Icon = def.icon
                                    const count = existingCount(def.type)
                                    const isSelected = selectedType === def.type
                                    return (
                                        <button
                                            key={def.type}
                                            type="button"
                                            className={cn(
                                                "flex w-full items-start gap-2.5 rounded-md border px-3 py-2.5 text-left transition-colors",
                                                isSelected
                                                    ? "border-primary bg-primary/5 ring-1 ring-primary/30"
                                                    : "border-transparent hover:bg-muted/50"
                                            )}
                                            onClick={() => handleSelectWidget(def)}
                                        >
                                            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted">
                                                <Icon className="h-4 w-4 text-foreground" />
                                            </div>
                                            <div className="min-w-0 flex-1">
                                                <div className="flex items-center gap-1.5">
                                                    <span className="text-sm font-medium text-foreground">
                                                        {def.label}
                                                    </span>
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
                                        </button>
                                    )
                                })}
                            </div>
                        </div>
                    ))}
                </div>
            </ScrollArea>
        </>
    )

    // ── Preview pane (shared between desktop and mobile) ────────────

    const previewContent = selectedDef && selectedSize ? (
        <div className="flex flex-1 flex-col overflow-hidden">
            {/* Back button (mobile only) */}
            {isMobile && (
                <div className="border-b px-4 py-2">
                    <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 text-xs"
                        onClick={() => setMobileTab("catalog")}
                    >
                        ← К списку виджетов
                    </Button>
                </div>
            )}

            {/* Preview area */}
            <ScrollArea className="flex-1">
            <div className="space-y-4 p-4">
                <div>
                    <div className="mb-2 flex items-center gap-2">
                        <selectedDef.icon className="h-4 w-4 text-muted-foreground" />
                        <h3 className="text-sm font-semibold text-foreground">{selectedDef.label}</h3>
                    </div>
                    <p className="text-xs text-muted-foreground">{selectedDef.description}</p>
                </div>

                {/* Live widget preview */}
                <WidgetPreviewContainer
                    definition={selectedDef}
                    config={previewConfig}
                    size={selectedSize}
                />

                {/* Size picker */}
                <div>
                    <p className="mb-2 text-xs font-medium text-muted-foreground">
                        Размер на дашборде
                    </p>
                    <div className="flex flex-wrap gap-1.5">
                        {selectedDef.allowedSizes.map((s) => (
                            <Button
                                key={s}
                                variant={selectedSize === s ? "default" : "outline"}
                                size="sm"
                                className="h-7 min-w-[48px] text-xs"
                                onClick={() => setSelectedSize(s)}
                            >
                                {SIZE_LABELS[s]}
                            </Button>
                        ))}
                    </div>
                </div>

                {/* Configuration panel */}
                <WidgetConfigPanel
                    widgetType={selectedDef.type}
                    config={previewConfig}
                    onConfigChange={handleConfigChange}
                />

                <Separator />

                {/* Add button + limit warning */}
                <div className="space-y-2">
                    <Button
                        className="w-full"
                        disabled={isLimitReached}
                        onClick={handleAdd}
                    >
                        <Plus className="mr-1.5 h-3.5 w-3.5" />
                        Добавить на дашборд
                    </Button>
                    {isLimitReached && (
                        <p className="text-center text-xs text-destructive">
                            Достигнут лимит виджетов ({MAX_WIDGETS})
                        </p>
                    )}
                </div>
            </div>
            </ScrollArea>
        </div>
    ) : (
        <div className="flex flex-1 items-center justify-center p-8">
            <div className="text-center">
                <Eye className="mx-auto h-8 w-8 text-muted-foreground/50" />
                <p className="mt-2 text-sm text-muted-foreground">
                    Выберите виджет из списка для предпросмотра
                </p>
            </div>
        </div>
    )

    // ── Desktop layout ──────────────────────────────────────────────

    if (!isMobile) {
        return (
            <Dialog open={open} onOpenChange={handleOpenChange}>
                <DialogContent className="max-w-[1100px] h-[85vh] max-h-[800px] p-0 flex flex-col gap-0">
                    <DialogHeader className="px-4 py-3 border-b shrink-0">
                        <DialogTitle>Добавить виджет</DialogTitle>
                        <DialogDescription>
                            Выберите виджет для размещения на дашборде
                        </DialogDescription>
                    </DialogHeader>
                    <div className="flex flex-1 overflow-hidden">
                        {/* Left: catalog */}
                        <div className="flex w-[280px] shrink-0 flex-col border-r">
                            {catalogContent}
                        </div>
                        {/* Right: preview + config */}
                        <div className="flex flex-1 flex-col overflow-hidden">
                            {previewContent}
                        </div>
                    </div>
                </DialogContent>
            </Dialog>
        )
    }

    // ── Mobile layout (fullscreen, tabs) ────────────────────────────

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent className="h-[100dvh] max-w-full p-0 flex flex-col gap-0 sm:rounded-none">
                <DialogHeader className="px-4 py-3 border-b shrink-0">
                    <DialogTitle>Добавить виджет</DialogTitle>
                    <DialogDescription>
                        Выберите виджет для размещения на дашборде
                    </DialogDescription>
                </DialogHeader>
                <div className="flex flex-1 flex-col overflow-hidden">
                    {mobileTab === "catalog" ? (
                        <div className="flex flex-1 flex-col overflow-hidden">
                            {catalogContent}
                        </div>
                    ) : (
                        previewContent
                    )}
                </div>
            </DialogContent>
        </Dialog>
    )
}

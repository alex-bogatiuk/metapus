"use client"
import { usePermissions } from "@/hooks/use-permissions"
import { useState } from "react"
import type { JsonObject } from "@/lib/json-value"
import { allWidgetDefinitions } from "@/lib/widget-registry"
import { CATEGORY_LABELS, SIZE_TO_WH, SIZE_LABELS, type WidgetCategory, type WidgetSize, type WidgetType } from "@/types/dashboard"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { useAuthStore, isSystemAdmin } from "@/stores/useAuthStore"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Field, FieldLabel } from "@/components/ui/field"
import { WidgetConfigPanel } from "./widget-config-panel"
import { WidgetPreviewContainer } from "./widget-preview-container"

export function WidgetGalleryDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const user = useAuthStore(s => s.user)
  const { has } = usePermissions()
  const definitions = allWidgetDefinitions.filter(d => (!d.adminOnly || isSystemAdmin(user)) && (!d.requiredPermission || has(d.requiredPermission)))
  const addWidget = useDashboardStore(s => s.addWidget)
  const layout = useDashboardStore(s => s.layout)
  const [filter, setFilter] = useState<WidgetCategory | "all">("all")
  const [type, setType] = useState<WidgetType>("attention-counter")
  const definition = definitions.find(d => d.type === type)
  const [config, setConfig] = useState<JsonObject>({ metric: "withdrawals" })
  const [size, setSize] = useState<WidgetSize>("2x1")
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="flex max-h-[90dvh] max-w-5xl flex-col gap-4 overflow-y-auto"><DialogHeader><DialogTitle>Добавить виджет</DialogTitle><DialogDescription>Выберите содержимое и размер. Можно добавить несколько экземпляров с разными настройками.</DialogDescription></DialogHeader>
    <ToggleGroup type="single" variant="outline" size="sm" value={filter} className="flex-wrap justify-start" onValueChange={v => { if (v) setFilter(v as WidgetCategory | "all") }}><ToggleGroupItem value="all">Все</ToggleGroupItem>{Object.entries(CATEGORY_LABELS).map(([v, l]) => <ToggleGroupItem key={v} value={v}>{l}</ToggleGroupItem>)}</ToggleGroup>
    <div className="grid min-h-0 gap-5 md:grid-cols-[260px_minmax(0,1fr)]"><div className="space-y-1">{definitions.filter(d => filter === "all" || d.category === filter).map(d => <Button key={d.type} variant={d.type === type ? "secondary" : "ghost"} className="h-auto w-full justify-start whitespace-normal py-3 text-left" onClick={() => { setType(d.type); setSize(d.defaultSize); setConfig({ ...d.defaultConfig }) }}><d.icon data-icon="inline-start" /><span>{d.label}</span></Button>)}</div>
      {definition && <div className="min-w-0 space-y-4"><p className="text-sm text-muted-foreground">{definition.description}</p><WidgetPreviewContainer definition={definition} config={config} size={size} /><Field><FieldLabel>Размер</FieldLabel><ToggleGroup type="single" variant="outline" value={size} className="flex-wrap justify-start" onValueChange={v => { if (definition.allowedSizes.includes(v as WidgetSize)) setSize(v as WidgetSize) }}>{definition.allowedSizes.map(s => <ToggleGroupItem key={s} value={s}>{SIZE_LABELS[s]}</ToggleGroupItem>)}</ToggleGroup></Field><WidgetConfigPanel widgetType={type} config={config} onConfigChange={setConfig} /><Button disabled={layout.widgets.length >= 20} onClick={() => { const wh = SIZE_TO_WH[size]; addWidget({ instanceId: `${type}-${crypto.randomUUID()}`, widgetType: type, x: 0, y: Math.max(0, ...layout.widgets.map(w => w.y + w.h)), ...wh, config }); onOpenChange(false) }}>Добавить на страницу</Button>{layout.widgets.length >= 20 && <p className="text-sm text-destructive">На странице уже 20 виджетов. Удалите один, чтобы добавить новый.</p>}</div>}
    </div>
  </DialogContent></Dialog>
}

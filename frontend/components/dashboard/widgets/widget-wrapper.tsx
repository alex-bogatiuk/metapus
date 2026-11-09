"use client"
import { usePermissions } from "@/hooks/use-permissions"
import { Suspense, useState } from "react"
import { ErrorBoundary } from "react-error-boundary"
import { X, GripVertical, Settings2 } from "lucide-react"
import { widgetRegistry } from "@/lib/widget-registry"
import { SIZE_TO_WH, SIZE_LABELS, type WidgetPlacement, type WidgetSize } from "@/types/dashboard"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { useAuthStore, isSystemAdmin } from "@/stores/useAuthStore"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@/components/ui/dialog"
import { Field, FieldLabel } from "@/components/ui/field"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { WidgetConfigPanel } from "./widget-config-panel"

export function WidgetWrapper({ placement, isEditMode }: { placement: WidgetPlacement; isEditMode: boolean }) {
  const { has } = usePermissions()
  const definition = widgetRegistry.get(placement.widgetType)
  const removeWidget = useDashboardStore(s => s.removeWidget)
  const updateWidgetConfig = useDashboardStore(s => s.updateWidgetConfig)
  const updatePositions = useDashboardStore(s => s.updatePositions)
  const layout = useDashboardStore(s => s.layout)
  const user = useAuthStore(s => s.user)
  const [open, setOpen] = useState(false)
  const [config, setConfig] = useState(placement.config)
  const [size, setSize] = useState<WidgetSize | null>(null)
  if (!definition || (definition.adminOnly && !isSystemAdmin(user)) || (definition?.requiredPermission && !has(definition.requiredPermission))) return <Card className="flex h-full items-center justify-center p-4 text-sm text-muted-foreground">Этот виджет доступен администратору системы.</Card>
  const Component = definition.component
  const settings = () => {
    setConfig(placement.config)
    setSize(definition.allowedSizes.find(s => SIZE_TO_WH[s].w === placement.w && SIZE_TO_WH[s].h === placement.h) ?? null)
    setOpen(true)
  }
  return <Card data-widget-type={placement.widgetType} data-widget-id={placement.instanceId} className="relative flex h-full min-w-0 flex-col overflow-hidden">
    {isEditMode && <div className="flex shrink-0 items-center justify-between border-b bg-muted/50 px-2 py-1">
      <span className="drag-handle flex cursor-grab items-center gap-1 text-xs text-muted-foreground"><GripVertical className="size-4" />{definition.label}</span>
      <div className="flex gap-1"><Button variant="ghost" size="icon" className="h-7 w-7" aria-label={`Настроить: ${definition.label}`} onClick={settings}><Settings2 /></Button><Button variant="ghost" size="icon" className="h-7 w-7" aria-label={`Удалить: ${definition.label}`} onClick={() => removeWidget(placement.instanceId)}><X /></Button></div>
    </div>}
    <ErrorBoundary fallbackRender={({ resetErrorBoundary }) => <div className="space-y-2 p-4"><p>Не удалось показать виджет.</p><Button variant="outline" size="sm" onClick={resetErrorBoundary}>Повторить</Button></div>} resetKeys={[placement.widgetType]}>
      <Suspense fallback={<Skeleton className="m-4 h-20" />}><Component placement={placement} config={placement.config} isEditMode={isEditMode} onConfigChange={next => updateWidgetConfig(placement.instanceId, next)} /></Suspense>
    </ErrorBoundary>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent><DialogHeader><DialogTitle>{definition.label}</DialogTitle><DialogDescription>Настройте размер и содержимое этого экземпляра.</DialogDescription></DialogHeader><Field><FieldLabel>Размер</FieldLabel><ToggleGroup type="single" variant="outline" value={size ?? ""} className="flex-wrap justify-start" onValueChange={v => { if (definition.allowedSizes.includes(v as WidgetSize)) setSize(v as WidgetSize) }}>{definition.allowedSizes.map(s => <ToggleGroupItem key={s} value={s}>{SIZE_LABELS[s]}</ToggleGroupItem>)}</ToggleGroup></Field>{size === null && <p className="text-xs text-muted-foreground">Сохранится размер, заданный вручную. Выберите вариант, чтобы изменить его.</p>}<WidgetConfigPanel widgetType={definition.type} config={config} onConfigChange={setConfig} /><DialogFooter><Button variant="outline" onClick={() => setOpen(false)}>Отмена</Button><Button onClick={() => { const wh = size ? SIZE_TO_WH[size] : { w: placement.w, h: placement.h }; updatePositions(layout.widgets.map(w => w.instanceId === placement.instanceId ? { ...w, ...wh, x: Math.min(w.x, 12 - wh.w), config } : w)); setOpen(false) }}>Применить</Button></DialogFooter></DialogContent></Dialog>
  </Card>
}

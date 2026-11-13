"use client"
import { useMemo } from "react"
import { ResponsiveGridLayout, useContainerWidth, type Layout } from "react-grid-layout"
import "react-grid-layout/css/styles.css"
import "react-resizable/css/styles.css"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { widgetRegistry } from "@/lib/widget-registry"
import { GRID_ROW_HEIGHT, GRID_MARGIN, SIZE_TO_WH } from "@/types/dashboard"
import { WidgetWrapper } from "./widget-wrapper"

export function WidgetGrid() {
  const layout = useDashboardStore(s => s.layout)
  const isEditMode = useDashboardStore(s => s.isEditMode)
  const updatePositions = useDashboardStore(s => s.updatePositions)
  const { width, containerRef, mounted } = useContainerWidth({ initialWidth: 1200 })
  const canMove = isEditMode && width >= 1200
  const gridLayout = useMemo(() => layout.widgets.map(w => {
    const sizes = widgetRegistry.get(w.widgetType)?.allowedSizes.map(s => SIZE_TO_WH[s]) ?? [{ w: 1, h: 1 }]
    return { i: w.instanceId, x: w.x, y: w.y, w: w.w, h: w.h,
      minW: canMove ? Math.min(...sizes.map(s => s.w)) : undefined,
      maxW: canMove ? Math.max(...sizes.map(s => s.w)) : undefined,
      minH: Math.min(...sizes.map(s => s.h)), maxH: Math.max(...sizes.map(s => s.h)), static: !canMove,
    }
  }), [layout.widgets, canMove])
  const savePositions = (next: Layout) => {
    if (!canMove) return
    const positions = new Map(next.map(w => [w.i, w]))
    updatePositions(layout.widgets.map(w => { const p = positions.get(w.instanceId); return p ? { ...w, x: p.x, y: p.y, w: p.w, h: p.h } : w }))
  }
  return <div ref={containerRef}>
    {isEditMode && !canMove && <p className="mb-3 text-xs text-muted-foreground">Перемещение и растягивание доступны при ширине области от 1200 px. Состав, размеры и настройки можно менять здесь.</p>}
    {mounted && <ResponsiveGridLayout className="widget-grid" width={width} layouts={{ lg: gridLayout }} breakpoints={{ lg: 1200, md: 996, sm: 600, xs: 0 }} cols={{ lg: 12, md: 8, sm: 4, xs: 1 }} rowHeight={GRID_ROW_HEIGHT} margin={[GRID_MARGIN, GRID_MARGIN]} containerPadding={[0, 0]} dragConfig={{ enabled: canMove, handle: ".drag-handle" }} resizeConfig={{ enabled: canMove }} onDragStop={savePositions} onResizeStop={savePositions}>
      {layout.widgets.map(w => <div key={w.instanceId}><WidgetWrapper placement={w} isEditMode={isEditMode} /></div>)}
    </ResponsiveGridLayout>}
  </div>
}

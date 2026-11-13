"use client"
import { useCallback, useMemo, useState } from "react"
import { ResponsiveGridLayout, useContainerWidth, type Layout } from "react-grid-layout"
import "react-grid-layout/css/styles.css"
import "react-resizable/css/styles.css"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { widgetRegistry } from "@/lib/widget-registry"
import { compactLayout, reflowLayout, type CompactRows } from "@/lib/dashboard-layout"
import { GRID_ROW_HEIGHT, GRID_MARGIN, SIZE_TO_WH } from "@/types/dashboard"
import { WidgetWrapper } from "./widget-wrapper"

const DESKTOP_MIN = 996
const FULL_HEIGHT: CompactRows = {}

export function WidgetGrid() {
  const layout = useDashboardStore(s => s.layout)
  const isEditMode = useDashboardStore(s => s.isEditMode)
  const updatePositions = useDashboardStore(s => s.updatePositions)
  const { width, containerRef, mounted } = useContainerWidth({ initialWidth: 1200 })
  // Calm widgets ask for fewer rows. While arranging, every widget shows its configured size.
  const [compactRows, setCompactRows] = useState<CompactRows>({})
  const reportCompactRows = useCallback((id: string, rows: number | null) => setCompactRows(prev => {
    if (rows == null) {
      if (!(id in prev)) return prev
      const next = { ...prev }
      delete next[id]
      return next
    }
    return prev[id] === rows ? prev : { ...prev, [id]: rows }
  }), [])
  const rows = isEditMode ? FULL_HEIGHT : compactRows
  // From 996 px the grid keeps the desktop 12 columns, so the saved layout is shown as it is and can be edited.
  const canMove = isEditMode && width >= DESKTOP_MIN
  const gridLayout = useMemo(() => layout.widgets.map(w => {
    const sizes = widgetRegistry.get(w.widgetType)?.allowedSizes.map(s => SIZE_TO_WH[s]) ?? [{ w: 1, h: 1 }]
    return { i: w.instanceId, x: w.x, y: w.y, w: w.w, h: w.h,
      minW: canMove ? Math.min(...sizes.map(s => s.w)) : undefined,
      maxW: canMove ? Math.max(...sizes.map(s => s.w)) : undefined,
      minH: Math.min(...sizes.map(s => s.h)), maxH: Math.max(...sizes.map(s => s.h)), static: !canMove,
    }
  }), [layout.widgets, canMove])
  // Narrow grids get the same widgets reflowed rather than cropped, so rows never have gaps.
  const layouts = useMemo(() => {
    const base = layout.widgets.map(w => ({ i: w.instanceId, x: w.x, y: w.y, w: w.w, h: w.h }))
    const narrow = (cols: number) => reflowLayout(base, cols, rows).map(item => ({ ...item, static: true }))
    // Outside edit mode the desktop layout is read-only, so calm bands may shrink.
    const lg = isEditMode ? gridLayout : compactLayout(base, rows).map(item => ({ ...item, static: true }))
    return { lg, sm: narrow(4), xs: narrow(1) }
  }, [layout.widgets, gridLayout, isEditMode, rows])
  const savePositions = (next: Layout) => {
    if (!canMove) return
    const positions = new Map(next.map(w => [w.i, w]))
    updatePositions(layout.widgets.map(w => { const p = positions.get(w.instanceId); return p ? { ...w, x: p.x, y: p.y, w: p.w, h: p.h } : w }))
  }
  return <div ref={containerRef}>
    {isEditMode && !canMove && <p className="mb-3 text-xs text-muted-foreground">Перемещение и растягивание доступны при ширине области от 996 px. Состав, размеры и настройки можно менять здесь.</p>}
    {mounted && <ResponsiveGridLayout className="widget-grid" width={width} layouts={layouts} breakpoints={{ lg: DESKTOP_MIN, sm: 480, xs: 0 }} cols={{ lg: 12, sm: 4, xs: 1 }} rowHeight={GRID_ROW_HEIGHT} margin={[GRID_MARGIN, GRID_MARGIN]} containerPadding={[0, 0]} dragConfig={{ enabled: canMove, handle: ".drag-handle" }} resizeConfig={{ enabled: canMove }} onDragStop={savePositions} onResizeStop={savePositions}>
      {layout.widgets.map(w => <div key={w.instanceId}><WidgetWrapper placement={w} isEditMode={isEditMode} onCompactRows={reportCompactRows} /></div>)}
    </ResponsiveGridLayout>}
  </div>
}

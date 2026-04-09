"use client"

import { useCallback, useMemo, useRef } from "react"
import { ResponsiveGridLayout, useContainerWidth, type Layout, type ResponsiveLayouts } from "react-grid-layout"
import "react-grid-layout/css/styles.css"
import "react-resizable/css/styles.css"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { WidgetWrapper } from "./widget-wrapper"
import type { WidgetPlacement } from "@/types/dashboard"

const BREAKPOINTS = { lg: 1200, md: 996, sm: 768, xs: 480 }
const COLS = { lg: 12, md: 8, sm: 4, xs: 1 }
const ROW_HEIGHT = 120
const MARGIN: [number, number] = [12, 12]

export function WidgetGrid() {
    const layout = useDashboardStore((s) => s.layout)
    const isEditMode = useDashboardStore((s) => s.isEditMode)
    const updatePositions = useDashboardStore((s) => s.updatePositions)
    const isInitialLoad = useRef(true)

    const { width, containerRef, mounted } = useContainerWidth({ initialWidth: 1200 })

    const gridLayout = useMemo(
        () =>
            layout.widgets.map((w) => ({
                i: w.instanceId,
                x: w.x,
                y: w.y,
                w: w.w,
                h: w.h,
                static: !isEditMode,
            })),
        [layout.widgets, isEditMode]
    )

    const handleLayoutChange = useCallback(
        (newLayout: Layout, _allLayouts: ResponsiveLayouts) => {
            if (isInitialLoad.current) {
                isInitialLoad.current = false
                return
            }
            if (!isEditMode) return

            const updated: WidgetPlacement[] = layout.widgets.map((widget) => {
                const item = newLayout.find((l) => l.i === widget.instanceId)
                if (!item) return widget
                return {
                    ...widget,
                    x: item.x,
                    y: item.y,
                    w: item.w,
                    h: item.h,
                }
            })

            updatePositions(updated)
        },
        [isEditMode, layout.widgets, updatePositions]
    )

    return (
        <div ref={containerRef}>
            {mounted && (
                <ResponsiveGridLayout
                    className="widget-grid"
                    width={width}
                    layouts={{ lg: gridLayout }}
                    breakpoints={BREAKPOINTS}
                    cols={COLS}
                    rowHeight={ROW_HEIGHT}
                    margin={MARGIN}
                    dragConfig={{ enabled: isEditMode, handle: ".drag-handle" }}
                    resizeConfig={{ enabled: isEditMode }}
                    onLayoutChange={handleLayoutChange}
                >
                    {layout.widgets.map((widget) => (
                        <div key={widget.instanceId}>
                            <WidgetWrapper
                                placement={widget}
                                isEditMode={isEditMode}
                            />
                        </div>
                    ))}
                </ResponsiveGridLayout>
            )}
        </div>
    )
}

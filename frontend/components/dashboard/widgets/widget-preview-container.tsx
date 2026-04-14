"use client"

import { Suspense, useMemo } from "react"
import { ErrorBoundary } from "react-error-boundary"
import { AlertTriangle, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { widgetConfigSchemas } from "@/types/dashboard"
import type { WidgetDefinition, WidgetPlacement, WidgetType, WidgetSize } from "@/types/dashboard"
import { GRID_ROW_HEIGHT, GRID_MARGIN, SIZE_TO_WH } from "@/types/dashboard"

interface WidgetPreviewContainerProps {
    definition: WidgetDefinition
    config: Record<string, unknown>
    size: WidgetSize
}

function PreviewErrorFallback({ error, resetErrorBoundary }: { error: unknown; resetErrorBoundary: () => void }) {
    const message = error instanceof Error ? error.message : String(error)
    return (
        <div className="flex h-full flex-col items-center justify-center gap-2 p-4 text-center">
            <AlertTriangle className="h-6 w-6 text-destructive" />
            <p className="text-sm font-medium text-destructive">Ошибка виджета</p>
            <p className="text-xs text-muted-foreground line-clamp-2">{message}</p>
            <Button variant="outline" size="sm" onClick={resetErrorBoundary}>
                Повторить
            </Button>
        </div>
    )
}

function PreviewLoadingFallback() {
    return (
        <div className="flex h-full items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
    )
}

export function WidgetPreviewContainer({ definition, config, size }: WidgetPreviewContainerProps) {
    const schema = widgetConfigSchemas[definition.type as WidgetType]
    const parseResult = schema.safeParse(config)
    const validConfig = parseResult.success ? (parseResult.data as Record<string, unknown>) : definition.defaultConfig

    const placement = useMemo<WidgetPlacement>(() => {
        const wh = SIZE_TO_WH[size]
        return {
            instanceId: "preview",
            widgetType: definition.type,
            x: 0,
            y: 0,
            w: wh.w,
            h: wh.h,
            config: validConfig,
        }
    }, [definition.type, size, validConfig])

    // Calculate preview height based on grid row height
    const wh = SIZE_TO_WH[size]
    const previewHeight = wh.h * GRID_ROW_HEIGHT + (wh.h - 1) * GRID_MARGIN

    const Component = definition.component

    return (
        <div
            className="overflow-hidden rounded-lg border bg-card shadow-sm transition-all duration-200"
            style={{ height: previewHeight, minHeight: 120 }}
        >
            <ErrorBoundary FallbackComponent={PreviewErrorFallback}>
                <Suspense fallback={<PreviewLoadingFallback />}>
                    <Component
                        placement={placement}
                        config={validConfig}
                        isEditMode={false}
                        onConfigChange={() => {}}
                    />
                </Suspense>
            </ErrorBoundary>
        </div>
    )
}

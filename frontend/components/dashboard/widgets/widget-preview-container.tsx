"use client"

import type { JsonObject } from "@/lib/json-value"

import { Suspense, useMemo } from "react"
import { ErrorBoundary, type FallbackProps } from "react-error-boundary"
import { AlertTriangle, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { RegisteredWidgetDefinition, WidgetPlacement, WidgetSize } from "@/types/dashboard"
import { SIZE_TO_WH, rowHeightPx } from "@/types/dashboard"

interface WidgetPreviewContainerProps {
    definition: RegisteredWidgetDefinition
    config: JsonObject
    size: WidgetSize
}

function PreviewErrorFallback({ error, resetErrorBoundary }: FallbackProps) {
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
    const placement = useMemo<WidgetPlacement>(
        () => ({ instanceId: "preview", widgetType: definition.type, size, config }),
        [definition.type, size, config],
    )

    // Calculate preview height based on grid row height
    const previewHeight = rowHeightPx(SIZE_TO_WH[size].h)

    const Component = definition.component

    return (
        <div
            className="flex flex-col overflow-hidden rounded-lg border bg-card shadow-sm transition-all duration-200"
            style={{ height: previewHeight, minHeight: 120 }}
        >
            <ErrorBoundary FallbackComponent={PreviewErrorFallback}>
                <Suspense fallback={<PreviewLoadingFallback />}>
                    <Component
                        placement={placement}
                        config={config}
                        isEditMode={false}
                        onConfigChange={() => {}}
                    />
                </Suspense>
            </ErrorBoundary>
        </div>
    )
}

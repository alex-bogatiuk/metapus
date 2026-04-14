"use client"

import { Suspense, useCallback } from "react"
import { ErrorBoundary } from "react-error-boundary"
import { AlertTriangle, Loader2, X, GripVertical } from "lucide-react"
import { cn } from "@/lib/utils"
import { widgetRegistry } from "@/lib/widget-registry"
import { widgetConfigSchemas } from "@/types/dashboard"
import type { WidgetPlacement, WidgetType } from "@/types/dashboard"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { Button } from "@/components/ui/button"

interface WidgetWrapperProps {
    placement: WidgetPlacement
    isEditMode: boolean
}

function ErrorFallback({ error, resetErrorBoundary }: { error: unknown; resetErrorBoundary: () => void }) {
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

function LoadingFallback() {
    return (
        <div className="flex h-full items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
    )
}

export function WidgetWrapper({ placement, isEditMode }: WidgetWrapperProps) {
    const definition = widgetRegistry.get(placement.widgetType)
    const removeWidget = useDashboardStore((s) => s.removeWidget)
    const updateWidgetConfig = useDashboardStore((s) => s.updateWidgetConfig)

    const handleConfigChange = useCallback(
        (config: Record<string, unknown>) => {
            updateWidgetConfig(placement.instanceId, config)
        },
        [placement.instanceId, updateWidgetConfig]
    )

    if (!definition) {
        return (
            <div className="flex h-full items-center justify-center rounded-lg border border-dashed bg-muted/50 p-4">
                <p className="text-xs text-muted-foreground">
                    Неизвестный виджет: {placement.widgetType}
                </p>
            </div>
        )
    }

    const schema = widgetConfigSchemas[placement.widgetType as WidgetType]
    const parseResult = schema.safeParse(placement.config)
    const validConfig = parseResult.success ? (parseResult.data as Record<string, unknown>) : definition.defaultConfig

    const Component = definition.component

    return (
        <div
            className={cn(
                "group relative flex h-full flex-col rounded-lg border bg-card shadow-sm transition-shadow",
                isEditMode && "ring-2 ring-primary/20 hover:ring-primary/40"
            )}
        >
            {isEditMode && (
                <>
                    <div className="drag-handle absolute left-1 top-1 z-10 cursor-grab rounded p-1 opacity-0 transition-opacity group-hover:opacity-100 hover:bg-muted">
                        <GripVertical className="h-4 w-4 text-muted-foreground" />
                    </div>
                    <Button
                        variant="ghost"
                        size="icon"
                        className="absolute right-1 top-1 z-10 h-6 w-6 opacity-0 transition-opacity group-hover:opacity-100"
                        onClick={() => removeWidget(placement.instanceId)}
                    >
                        <X className="h-3.5 w-3.5" />
                    </Button>
                </>
            )}

            <div className={cn(
                "flex-1 overflow-hidden transition-[padding] duration-200",
                isEditMode && "pointer-events-none opacity-60 group-hover:pt-7"
            )}>
                <ErrorBoundary FallbackComponent={ErrorFallback}>
                    <Suspense fallback={<LoadingFallback />}>
                        <Component
                            placement={placement}
                            config={validConfig}
                            isEditMode={isEditMode}
                            onConfigChange={handleConfigChange}
                        />
                    </Suspense>
                </ErrorBoundary>
            </div>
        </div>
    )
}

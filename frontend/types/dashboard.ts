/**
 * Dashboard widget system types + Zod config schemas.
 *
 * Each widget type has a strictly typed config validated at runtime.
 * Adding a new widget? Add its Zod schema here and entry in WidgetConfigMap.
 */

import type { LucideIcon } from "lucide-react"
import { z } from "zod"

// ── Widget Config Schemas (Zod) ─────────────────────────────────────

export const kpiConfigSchema = z.object({
    metric: z.enum([
        "cash-balance", "stock-value", "receivables",
        "payables", "net-assets", "leads-count",
        "receipts-period", "sales-period", "payments-period",
    ]),
})

export const recentDocsConfigSchema = z.object({
    limit: z.number().int().min(1).max(20).default(5),
    documentTypes: z.array(z.string()).default([]),
})

export const chartConfigSchema = z.object({
    chartType: z.enum(["bar", "line", "pie"]).default("bar"),
    warehouseIds: z.array(z.string()).default([]),
    period: z.enum(["week", "month", "quarter", "year"]).default("month"),
})

export const quickActionsConfigSchema = z.object({
    actions: z.array(z.object({
        label: z.string(),
        href: z.string(),
        icon: z.string(),
    })).optional(),
})

export const tasksConfigSchema = z.object({})

export const eventLogConfigSchema = z.object({
    limit: z.number().int().min(1).max(20).default(5),
    eventTypes: z.array(z.string()).optional(),
})

// ── Config Type Map ─────────────────────────────────────────────────

export type WidgetConfigMap = {
    kpi: z.infer<typeof kpiConfigSchema>
    "recent-documents": z.infer<typeof recentDocsConfigSchema>
    "stock-chart": z.infer<typeof chartConfigSchema>
    "quick-actions": z.infer<typeof quickActionsConfigSchema>
    tasks: z.infer<typeof tasksConfigSchema>
    "event-log": z.infer<typeof eventLogConfigSchema>
}

export type WidgetType = keyof WidgetConfigMap

// Runtime schema lookup
export const widgetConfigSchemas: Record<WidgetType, z.ZodType> = {
    kpi: kpiConfigSchema,
    "recent-documents": recentDocsConfigSchema,
    "stock-chart": chartConfigSchema,
    "quick-actions": quickActionsConfigSchema,
    tasks: tasksConfigSchema,
    "event-log": eventLogConfigSchema,
}

// ── Core Types ──────────────────────────────────────────────────────

export type WidgetCategory = "kpi" | "lists" | "charts" | "actions" | "system"
export type WidgetSize = "2x1" | "3x1" | "4x1" | "2x2" | "3x2" | "4x2" | "4x3" | "4x4"

export interface WidgetDefinition<T extends WidgetType = WidgetType> {
    type: T
    label: string
    description: string
    icon: LucideIcon
    allowedSizes: WidgetSize[]
    defaultSize: WidgetSize
    category: WidgetCategory
    defaultConfig: WidgetConfigMap[T]
    /** Permission required to see this widget's data. Null = no restriction. */
    requiredPermission: string | null
    // Type-erased: runtime safety ensured by Zod validation in WidgetWrapper
    component: React.LazyExoticComponent<React.ComponentType<any>>
}

export interface WidgetPlacement {
    instanceId: string
    widgetType: WidgetType
    x: number; y: number; w: number; h: number
    config: Record<string, unknown>
}

export interface DashboardLayout {
    version: number
    widgets: WidgetPlacement[]
}

export interface WidgetRenderProps<T extends WidgetType = WidgetType> {
    placement: WidgetPlacement
    config: WidgetConfigMap[T]
    isEditMode: boolean
    onConfigChange: (config: WidgetConfigMap[T]) => void
}

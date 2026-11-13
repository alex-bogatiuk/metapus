import type { JsonObject } from "@/lib/json-value"
/**
 * Dashboard widget system types + Zod config schemas.
 *
 * Each widget type has a strictly typed config validated at runtime.
 * Adding a new widget? Add its Zod schema here and entry in WidgetConfigMap.
 */

import type { LucideIcon } from "lucide-react"
import { z } from "zod"

export const counterConfigSchema = z.object({ metric: z.enum(["withdrawals", "inbound", "outflows", "networks"]) })
export const queueConfigSchema = z.object({ kind: z.enum(["all", "withdrawal", "inbound", "outflow"]).default("all"), limit: z.int().min(1).max(20).default(5) })
export const healthConfigSchema = z.object({ showWorkerStats: z.boolean().default(true) })
export const financeConfigSchema = z.object({ view: z.enum(["obligations", "wallets"]).default("obligations") })
export const reconciliationConfigSchema = z.object({ limit: z.int().min(1).max(20).default(8) })
export const analyticsConfigSchema = z.object({ period: z.enum(["7d", "30d", "90d"]).default("30d"), tokenId: z.string().default("") })

export type WidgetConfigMap = {
  "attention-counter": z.infer<typeof counterConfigSchema>
  "attention-queue": z.infer<typeof queueConfigSchema>
  "processing-health": z.infer<typeof healthConfigSchema>
  "financial-summary": z.infer<typeof financeConfigSchema>
  "custody-reconciliation": z.infer<typeof reconciliationConfigSchema>
  "receipts-volume": z.infer<typeof analyticsConfigSchema>
  "platform-fees": z.infer<typeof analyticsConfigSchema>
  "payment-results": z.infer<typeof analyticsConfigSchema>
}
export type WidgetType = keyof WidgetConfigMap
export const widgetConfigSchemas: { [K in WidgetType]: z.ZodType<WidgetConfigMap[K]> } = {
  "attention-counter": counterConfigSchema,
  "attention-queue": queueConfigSchema,
  "processing-health": healthConfigSchema,
  "financial-summary": financeConfigSchema,
  "custody-reconciliation": reconciliationConfigSchema,
  "receipts-volume": analyticsConfigSchema,
  "platform-fees": analyticsConfigSchema,
  "payment-results": analyticsConfigSchema,
}

// ── Core Types ──────────────────────────────────────────────────────

export type WidgetCategory = "kpi" | "lists" | "charts" | "system"
export type WidgetSize = "2x1" | "3x1" | "4x1" | "2x2" | "3x2" | "4x2" | "4x3" | "4x4" | "2x3" | "4x4-compact" | "fullx3" | "3x3" | "2x4"

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
  adminOnly?: boolean
  // Registration binds this component to the schema for its exact widget type.
  component: React.LazyExoticComponent<React.ComponentType<WidgetRenderProps<T>>>
}

export interface WidgetPlacement {
  instanceId: string
  widgetType: WidgetType
  /** One of the widget's allowed sizes: its share of a 12-column row and its height in row units. */
  size: WidgetSize
  config: JsonObject
}

/**
 * The dashboard is a list of rows; a row is a list of widgets read left to right.
 * The widths in a row add up to at most 12 columns. A row that is not full stretches
 * its widgets proportionally, so a row never has a gap. A row is as tall as its tallest widget.
 */
export interface DashboardLayout {
  version: number
  catalogVersion: 3
  rows: WidgetPlacement[][]
}

export interface WidgetRenderProps<T extends WidgetType = WidgetType> {
  placement: WidgetPlacement
  config: WidgetConfigMap[T]
  isEditMode: boolean
  onConfigChange: (config: WidgetConfigMap[T]) => void
}

// ── Shared Constants ────────────────────────────────────────────────

export const CATEGORY_LABELS: Record<WidgetCategory, string> = {
  kpi: "Показатели",
  lists: "Списки",
  charts: "Графики",
  system: "Система",
}

export const SIZE_TO_WH: Record<WidgetSize, { w: number; h: number }> = {
  "2x4": { w: 4, h: 4 },
  "3x3": { w: 6, h: 3 },
  "2x3": { w: 4, h: 3 },
  "4x4-compact": { w: 8, h: 4 },
  "fullx3": { w: 12, h: 3 },
  "2x1": { w: 3, h: 1 },
  "3x1": { w: 4, h: 1 },
  "4x1": { w: 6, h: 1 },
  "2x2": { w: 4, h: 2 },
  "3x2": { w: 6, h: 2 },
  "4x2": { w: 8, h: 2 },
  "4x3": { w: 8, h: 3 },
  "4x4": { w: 12, h: 4 },
}

export const SIZE_LABELS: Record<WidgetSize, string> = {
  "2x4": "⅓ ширины · большой",
  "3x3": "½ ширины · высокий",
  "2x3": "⅓ ширины · высокий",
  "4x4-compact": "⅔ ширины · высокий",
  "fullx3": "Во всю ширину",
  "2x1": "¼ ширины",
  "3x1": "⅓ ширины",
  "4x1": "½ ширины",
  "2x2": "⅓ ширины · средний",
  "3x2": "½ ширины · средний",
  "4x2": "⅔ ширины · средний",
  "4x3": "⅔ ширины",
  "4x4": "Во всю ширину · высокий",
}

export const GRID_ROW_HEIGHT = 120
export const GRID_MARGIN = 12
/** Columns in a desktop row. */
export const ROW_COLUMNS = 12

/** Pixel height of a row that is `units` rows tall, gaps between units included. */
export const rowHeightPx = (units: number) => units * GRID_ROW_HEIGHT + (units - 1) * GRID_MARGIN

/** Runtime registry entry closes over a concrete typed component and config schema. */
export interface RegisteredWidgetDefinition extends Omit<WidgetDefinition, "component" | "defaultConfig"> {
  defaultConfig: JsonObject
  component: React.ComponentType<WidgetRuntimeProps>
}
export interface WidgetRuntimeProps {
  placement: WidgetPlacement
  config: JsonObject
  isEditMode: boolean
  onConfigChange: (config: JsonObject) => void
}

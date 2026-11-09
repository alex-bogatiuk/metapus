import { z } from "zod"
import { jsonObjectSchema, jsonObjectSnapshot, jsonValueSchema } from "./json-value"
import { widgetConfigSchemas, type DashboardLayout, type WidgetPlacement, type WidgetType } from "../types/dashboard"

export function defaultDashboardLayout(): DashboardLayout {
  const widgets: WidgetPlacement[] = []
  const add = (type: WidgetType, key: string, x: number, y: number, w: number, h: number, config: WidgetPlacement["config"]) =>
    widgets.push({ instanceId: `default-${key}`, widgetType: type, x, y, w, h, config })
  ;["withdrawals", "inbound", "outflows", "networks"].forEach((metric, i) => add("attention-counter", metric, i * 3, 0, 3, 1, { metric }))
  add("attention-queue", "queue", 0, 1, 8, 3, { kind: "all", limit: 5 })
  add("processing-health", "health", 8, 1, 4, 3, { showWorkerStats: true })
  add("financial-summary", "obligations", 0, 4, 6, 3, { view: "obligations" })
  add("financial-summary", "wallets", 6, 4, 6, 3, { view: "wallets" })
  add("custody-reconciliation", "reconciliation", 0, 7, 12, 3, { limit: 8 })
  add("receipts-volume", "receipts", 0, 10, 8, 4, { period: "30d", tokenId: "" })
  add("platform-fees", "fees", 8, 10, 4, 2, { period: "30d", tokenId: "" })
  add("payment-results", "results", 8, 12, 4, 4, { period: "30d", tokenId: "" })
  return { catalogVersion: 2, version: 1, widgets }
}

const rawLayoutSchema = z.object({
  version: z.int().nonnegative(), catalogVersion: z.number().optional(),
  widgets: z.array(z.object({ instanceId: z.string().min(1), widgetType: z.string(),
    x: z.number().finite(), y: z.number().finite(), w: z.number().finite(), h: z.number().finite(), config: jsonObjectSchema,
  })).max(20),
})
/** Old catalog entries are retired together. A deliberately empty new layout stays empty. */
export function normalizeDashboardLayout<T>(value: T): DashboardLayout {
  const parsed = rawLayoutSchema.safeParse(value)
  if (!parsed.success || parsed.data.widgets.some(w => !Object.hasOwn(widgetConfigSchemas, w.widgetType))) return defaultDashboardLayout()
  const seen = new Set<string>()
  const widgets: WidgetPlacement[] = []
  for (const w of parsed.data.widgets) {
    if (seen.has(w.instanceId) || w.w < 1 || w.h < 1 || w.x < 0 || w.y < 0) return defaultDashboardLayout()
    seen.add(w.instanceId)
    const type = w.widgetType as WidgetType
    const config = widgetConfigSchemas[type].safeParse(w.config)
    if (!config.success) return defaultDashboardLayout()
    const width = Math.min(12, Math.max(1, Math.round(w.w)))
    widgets.push({ ...w, widgetType: type, x: Math.min(12 - width, Math.round(w.x)), y: Math.round(w.y),
      w: width, h: Math.min(8, Math.max(1, Math.round(w.h))), config: jsonObjectSnapshot(config.data) })
  }
  return { version: parsed.data.version, catalogVersion: 2, widgets }
}

export function persistedDashboardLayout<T>(value: T): DashboardLayout {
  const parsed = z.object({ layout: jsonValueSchema }).safeParse(value)
  return normalizeDashboardLayout(parsed.success ? parsed.data.layout : null)
}

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
  add("payment-results", "results", 8, 12, 4, 2, { period: "30d", tokenId: "" })
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

/** A grid item as react-grid-layout places it; `i` is the widget instance id. */
export interface GridItem { i: string; x: number; y: number; w: number; h: number }

/** Rows a calm widget asks for, by instance id. A widget never grows past its configured height. */
export type CompactRows = Readonly<Record<string, number>>

const effective = (item: GridItem, rows: CompactRows) => Math.max(1, Math.min(item.h, rows[item.i] ?? item.h))

/**
 * Shrinks the desktop layout where widgets have nothing to show, without opening gaps.
 * The layout is cut into horizontal bands (widgets whose rows overlap). A band shrinks only
 * when every widget in it spans the whole band: its height becomes the tallest request among them.
 * Bands with stacked widgets keep their height. Bands below move up by what was saved.
 */
export function compactLayout(items: readonly GridItem[], rows: CompactRows): GridItem[] {
  const order = [...items].sort((a, b) => a.y - b.y || a.x - b.x)
  const bands: { top: number; bottom: number; items: GridItem[] }[] = []
  for (const item of order) {
    const band = bands[bands.length - 1]
    if (band && item.y < band.bottom) {
      band.items.push(item)
      band.bottom = Math.max(band.bottom, item.y + item.h)
    } else {
      bands.push({ top: item.y, bottom: item.y + item.h, items: [item] })
    }
  }
  const out: GridItem[] = []
  let y = 0
  for (const band of bands) {
    const flat = band.items.every(item => item.y === band.top && item.y + item.h === band.bottom)
    const height = flat ? Math.max(...band.items.map(item => effective(item, rows))) : band.bottom - band.top
    for (const item of band.items) out.push({ ...item, y: y + item.y - band.top, h: flat ? height : item.h })
    y += height
  }
  return out
}

/** Desktop widths at or above this many of 12 columns span a whole row when the grid narrows. */
const WIDE_SPAN = 8

/**
 * Reflows the desktop (12-column) layout for a narrower grid instead of cropping it,
 * so no row is left with a gap. Widgets keep their reading order (top to bottom, left to right):
 * wide ones take the full row; the rest pair up two per row when their configured heights match;
 * a widget without a pair takes the full row. With one column everything stacks.
 * Each row is as tall as its widgets need (`rows` lets calm widgets ask for less).
 */
export function reflowLayout(items: readonly GridItem[], cols: number, rows: CompactRows = {}): GridItem[] {
  const order = [...items].sort((a, b) => a.y - b.y || a.x - b.x)
  const out: GridItem[] = []
  let y = 0
  const full = (item: GridItem) => {
    const h = effective(item, rows)
    out.push({ i: item.i, x: 0, y, w: cols, h })
    y += h
  }
  if (cols < 2) {
    order.forEach(full)
    return out
  }
  const half = Math.floor(cols / 2)
  let pending: GridItem | null = null
  for (const item of order) {
    if (item.w >= WIDE_SPAN) {
      if (pending) full(pending)
      pending = null
      full(item)
    } else if (!pending) {
      pending = item
    } else if (pending.h === item.h) {
      const h = Math.max(effective(pending, rows), effective(item, rows))
      out.push({ i: pending.i, x: 0, y, w: half, h }, { i: item.i, x: half, y, w: cols - half, h })
      y += h
      pending = null
    } else {
      full(pending)
      pending = item
    }
  }
  if (pending) full(pending)
  return out
}

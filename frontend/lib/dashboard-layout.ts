import { z } from "zod"
import { jsonObjectSchema, jsonObjectSnapshot, jsonValueSchema, type JsonObject } from "./json-value"
import { allWidgetDefinitions } from "./widget-registry"
import { ROW_COLUMNS, SIZE_TO_WH, widgetConfigSchemas, type DashboardLayout, type WidgetPlacement, type WidgetSize, type WidgetType } from "../types/dashboard"

/** Most widgets a page may hold. */
export const MAX_WIDGETS = 20

type Rows = WidgetPlacement[][]

const definitionOf = (type: WidgetType) => allWidgetDefinitions.find(d => d.type === type)
/** Columns a widget takes in a full desktop row. */
export const spanOf = (size: WidgetSize) => SIZE_TO_WH[size].w
/** Columns a row's widgets ask for together. */
export const rowSpan = (row: readonly WidgetPlacement[]) => row.reduce((sum, w) => sum + spanOf(w.size), 0)
export const layoutWidgets = (layout: DashboardLayout) => layout.rows.flat()

export function defaultDashboardLayout(): DashboardLayout {
  const widget = (key: string, widgetType: WidgetType, size: WidgetSize, config: WidgetPlacement["config"]): WidgetPlacement =>
    ({ instanceId: `default-${key}`, widgetType, size, config })
  return { catalogVersion: 3, version: 1, rows: [
    ["withdrawals", "inbound", "outflows", "networks"].map(metric => widget(metric, "attention-counter", "2x1", { metric })),
    [widget("queue", "attention-queue", "4x3", { kind: "all", limit: 5 }), widget("health", "processing-health", "2x3", { showWorkerStats: true })],
    [widget("obligations", "financial-summary", "3x3", { view: "obligations" }), widget("wallets", "financial-summary", "3x3", { view: "wallets" })],
    [widget("reconciliation", "custody-reconciliation", "fullx3", { limit: 8 })],
    [widget("receipts", "receipts-volume", "4x4", { period: "30d", tokenId: "" })],
    [widget("fees", "platform-fees", "3x2", { period: "30d", tokenId: "" }), widget("results", "payment-results", "3x2", { period: "30d", tokenId: "" })],
  ] }
}

// ── Reading a stored layout ─────────────────────────────────────────

const rowsLayoutSchema = z.object({
  version: z.int().nonnegative(), catalogVersion: z.literal(3),
  rows: z.array(z.array(z.object({ instanceId: z.string().min(1), widgetType: z.string(), size: z.string(), config: jsonObjectSchema }))),
})
/** Catalog 2 placed widgets by free x/y/w/h coordinates on a 12-column grid. */
const positionsLayoutSchema = z.object({
  version: z.int().nonnegative(), catalogVersion: z.number().optional(),
  widgets: z.array(z.object({ instanceId: z.string().min(1), widgetType: z.string(),
    x: z.number().finite(), y: z.number().finite(), w: z.number().finite(), h: z.number().finite(), config: jsonObjectSchema,
  })),
})

/** The allowed size closest to a free width × height, width weighing more than height. */
export function nearestSize(type: WidgetType, w: number, h: number): WidgetSize {
  const allowed = definitionOf(type)?.allowedSizes ?? ["2x1"]
  return allowed.reduce((best, size) => {
    const cost = (s: WidgetSize) => Math.abs(SIZE_TO_WH[s].w - w) * 4 + Math.abs(SIZE_TO_WH[s].h - h)
    return cost(size) < cost(best) ? size : best
  })
}

/** Splits rows that ask for more than 12 columns, keeping order, and drops empty rows. */
export function fitRows(rows: Rows): Rows {
  const out: Rows = []
  for (const row of rows) {
    let current: WidgetPlacement[] = []
    for (const widget of row) {
      if (current.length > 0 && rowSpan(current) + spanOf(widget.size) > ROW_COLUMNS) {
        out.push(current)
        current = []
      }
      current.push(widget)
    }
    if (current.length > 0) out.push(current)
  }
  return out
}

/**
 * Turns catalog-2 coordinates into rows: widgets whose rows overlap form a band,
 * and each distinct top edge inside a band starts a row read left to right.
 */
function rowsFromPositions(items: readonly (WidgetPlacement & { x: number; y: number; h: number })[]): Rows {
  const order = [...items].sort((a, b) => a.y - b.y || a.x - b.x)
  const rows: Rows = []
  let bandBottom = -1
  let band: typeof order = []
  const flush = () => {
    const tops = [...new Set(band.map(i => i.y))].sort((a, b) => a - b)
    for (const top of tops) rows.push(band.filter(i => i.y === top).sort((a, b) => a.x - b.x).map(({ instanceId, widgetType, size, config }) => ({ instanceId, widgetType, size, config })))
    band = []
  }
  for (const item of order) {
    if (band.length > 0 && item.y >= bandBottom) flush()
    band.push(item)
    bandBottom = Math.max(bandBottom, item.y + item.h)
  }
  if (band.length > 0) flush()
  return fitRows(rows)
}

/** A widget is kept only with a known type, an allowed size and a valid config. */
function checkedWidget(raw: { instanceId: string; widgetType: string; config: JsonObject }, size: (type: WidgetType) => WidgetSize | null): WidgetPlacement | null {
  if (!Object.hasOwn(widgetConfigSchemas, raw.widgetType)) return null
  const widgetType = raw.widgetType as WidgetType
  const config = widgetConfigSchemas[widgetType].safeParse(raw.config)
  const chosen = size(widgetType)
  if (!config.success || !chosen) return null
  return { instanceId: raw.instanceId, widgetType, size: chosen, config: jsonObjectSnapshot(config.data) }
}

/**
 * Old catalogs are retired together, and a broken layout is never partly restored:
 * both fall back to the default. Catalog-2 coordinates are converted to rows.
 * A deliberately empty layout stays empty.
 */
export function normalizeDashboardLayout<T>(value: T): DashboardLayout {
  const rowsLayout = rowsLayoutSchema.safeParse(value)
  if (rowsLayout.success) {
    const rows: Rows = []
    for (const row of rowsLayout.data.rows) {
      const widgets: WidgetPlacement[] = []
      for (const raw of row) {
        const widget = checkedWidget(raw, type => {
          const allowed = definitionOf(type)?.allowedSizes ?? []
          return allowed.includes(raw.size as WidgetSize) ? raw.size as WidgetSize : null
        })
        if (!widget) return defaultDashboardLayout()
        widgets.push(widget)
      }
      rows.push(widgets)
    }
    return sealed(rowsLayout.data.version, rows)
  }
  const positions = positionsLayoutSchema.safeParse(value)
  if (!positions.success) return defaultDashboardLayout()
  const items: (WidgetPlacement & { x: number; y: number; h: number })[] = []
  for (const raw of positions.data.widgets) {
    if (raw.w < 1 || raw.h < 1 || raw.x < 0 || raw.y < 0) return defaultDashboardLayout()
    const widget = checkedWidget(raw, type => nearestSize(type, Math.round(raw.w), Math.round(raw.h)))
    if (!widget) return defaultDashboardLayout()
    items.push({ ...widget, x: Math.round(raw.x), y: Math.round(raw.y), h: Math.max(1, Math.round(raw.h)) })
  }
  return sealed(positions.data.version, rowsFromPositions(items))
}

function sealed(version: number, rows: Rows): DashboardLayout {
  const ids = rows.flat().map(w => w.instanceId)
  if (new Set(ids).size !== ids.length || ids.length > MAX_WIDGETS) return defaultDashboardLayout()
  return { version, catalogVersion: 3, rows: fitRows(rows) }
}

export function persistedDashboardLayout<T>(value: T): DashboardLayout {
  const parsed = z.object({ layout: jsonValueSchema }).safeParse(value)
  return normalizeDashboardLayout(parsed.success ? parsed.data.layout : null)
}

// ── Editing rows ────────────────────────────────────────────────────

/** A new widget joins the last row when it fits there, otherwise it starts a row. */
export function placeWidget(rows: Rows, widget: WidgetPlacement): Rows {
  const last = rows[rows.length - 1]
  if (last && rowSpan(last) + spanOf(widget.size) <= ROW_COLUMNS) return [...rows.slice(0, -1), [...last, widget]]
  return [...rows, [widget]]
}

export function removeWidget(rows: Rows, id: string): Rows {
  return fitRows(rows.map(row => row.filter(w => w.instanceId !== id)))
}

/** A change that makes a row too wide moves the widgets that no longer fit to a row of their own below. */
export function updateWidget(rows: Rows, id: string, patch: Partial<Pick<WidgetPlacement, "size" | "config">>): Rows {
  return fitRows(rows.map(row => row.map(w => w.instanceId === id ? { ...w, ...patch } : w)))
}

// ── Showing rows on a screen ────────────────────────────────────────

/** Rows a calm widget asks for, by instance id. A widget never grows past its configured height. */
export type CompactRows = Readonly<Record<string, number>>

/** wide: desktop rows as stored; two: a row splits into lines of two; one: everything stacks. */
export type LayoutMode = "wide" | "two" | "one"

export interface LineCell { widget: WidgetPlacement; span: number }
/** One horizontal line on screen: `columns` grid tracks, `height` in row units. */
export interface LayoutLine { key: string; row: number; columns: number; height: number; cells: LineCell[] }

const heightOf = (widget: WidgetPlacement, compact: CompactRows) => {
  const configured = SIZE_TO_WH[widget.size].h
  return Math.max(1, Math.min(configured, compact[widget.instanceId] ?? configured))
}

/** Shares `total` columns in proportion to `weights`, whole columns only, largest remainders first. */
export function distributeColumns(weights: readonly number[], total = ROW_COLUMNS): number[] {
  const sum = weights.reduce((a, b) => a + b, 0)
  const exact = weights.map(w => w * total / sum)
  const whole = exact.map(Math.floor)
  let rest = total - whole.reduce((a, b) => a + b, 0)
  const byRemainder = exact.map((value, i) => ({ i, remainder: value - whole[i] })).sort((a, b) => b.remainder - a.remainder || a.i - b.i)
  for (const { i } of byRemainder) {
    if (rest-- <= 0) break
    whole[i]++
  }
  return whole
}

/** Desktop widths at or above this many of 12 columns keep a whole line on narrow screens. */
const WIDE_SPAN = 8

/**
 * Lines to draw for the rows on a screen of the given mode. Every line is full, so nothing is ever left
 * empty beside a widget. A line is as tall as its tallest widget after calm widgets asked for less.
 */
export function layoutLines(rows: readonly WidgetPlacement[][], mode: LayoutMode, compact: CompactRows = {}): LayoutLine[] {
  const lines: LayoutLine[] = []
  rows.forEach((row, r) => {
    // A row emptied mid-drag draws nothing; indices stay those of the stored rows.
    if (row.length === 0) return
    if (mode === "wide") {
      const spans = distributeColumns(row.map(w => spanOf(w.size)))
      lines.push({ key: `row-${r}`, row: r, columns: ROW_COLUMNS, height: Math.max(...row.map(w => heightOf(w, compact))),
        cells: row.map((widget, i) => ({ widget, span: spans[i] })) })
      return
    }
    const line = (cells: WidgetPlacement[], columns: number) => lines.push({
      key: `row-${r}-${cells[0].instanceId}`, row: r, columns, height: Math.max(...cells.map(w => heightOf(w, compact))),
      cells: cells.map(widget => ({ widget, span: columns / cells.length })),
    })
    if (mode === "one") {
      row.forEach(widget => line([widget], 1))
      return
    }
    let pending: WidgetPlacement | null = null
    for (const widget of row) {
      if (spanOf(widget.size) >= WIDE_SPAN) {
        if (pending) line([pending], 2)
        pending = null
        line([widget], 2)
      } else if (pending) {
        line([pending, widget], 2)
        pending = null
      } else {
        pending = widget
      }
    }
    if (pending) line([pending], 2)
  })
  return lines
}

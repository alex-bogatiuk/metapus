import { z } from "zod"
import { jsonObjectSchema, jsonObjectSnapshot, jsonValueSchema } from "./json-value"
import { allWidgetDefinitions } from "./widget-registry"
import { ROW_COLUMNS, SIZE_TO_WH, type DashboardLayout, type WidgetPlacement, type WidgetSize, type WidgetType } from "../types/dashboard"

/** Most widgets a page may hold. */
export const MAX_WIDGETS = 20

type Rows = WidgetPlacement[][]

const definitionOf = (type: WidgetType) => allWidgetDefinitions.find(d => d.type === type)
/** Columns a widget takes in a full desktop row. */
export const spanOf = (size: WidgetSize) => SIZE_TO_WH[size].w
/** Columns a row's widgets ask for together. */
export const rowSpan = (row: readonly WidgetPlacement[]) => row.reduce((sum, w) => sum + spanOf(w.size), 0)
export const layoutWidgets = (layout: DashboardLayout) => layout.rows.flat()

let defaultRows: () => Rows = () => []

/**
 * Sets the dashboard a user gets before arranging their own: the
 * configuration knows its widgets, the platform starts empty.
 */
export function registerDefaultDashboard(rows: () => Rows): void {
  defaultRows = rows
}

export function defaultDashboardLayout(): DashboardLayout {
  return { catalogVersion: 3, version: 1, rows: defaultRows() }
}

// ── Reading a stored layout ─────────────────────────────────────────

const storedLayoutSchema = z.object({
  version: z.int().nonnegative(), catalogVersion: z.literal(3),
  rows: z.array(z.array(z.object({ instanceId: z.string().min(1), widgetType: z.string(), size: z.string(), config: jsonObjectSchema }))),
})

/** A widget is kept only with a known type, a size its definition allows and a valid config. */
function checkedWidget(raw: z.infer<typeof storedLayoutSchema>["rows"][number][number]): WidgetPlacement | null {
  const definition = definitionOf(raw.widgetType)
  const config = definition?.parseConfig(raw.config)
  const size = raw.size as WidgetSize
  if (!definition || !config || !definition.allowedSizes.includes(size)) return null
  return { instanceId: raw.instanceId, widgetType: definition.type, size, config }
}

/**
 * Reads a stored layout. Anything that is not a valid layout of the current
 * catalog is replaced by the default as a whole; a deliberately empty layout stays empty.
 */
export function readDashboardLayout<T>(value: T): DashboardLayout {
  const stored = storedLayoutSchema.safeParse(value)
  if (!stored.success) return defaultDashboardLayout()
  const rows: Rows = []
  for (const row of stored.data.rows) {
    const widgets: WidgetPlacement[] = []
    for (const raw of row) {
      const widget = checkedWidget(raw)
      if (!widget) return defaultDashboardLayout()
      widgets.push(widget)
    }
    rows.push(widgets)
  }
  const ids = rows.flat().map(w => w.instanceId)
  if (new Set(ids).size !== ids.length || ids.length > MAX_WIDGETS) return defaultDashboardLayout()
  return { version: stored.data.version, catalogVersion: 3, rows: fitRows(rows) }
}

const browserCopySchema = z.object({ layout: jsonValueSchema })

/** Reads the copy kept in browser storage, which is untrusted like the server copy. */
export function readBrowserDashboardLayout<T>(persisted: T): DashboardLayout {
  const copy = browserCopySchema.safeParse(persisted)
  return readDashboardLayout(copy.success ? copy.data.layout : null)
}

// ── Editing rows ────────────────────────────────────────────────────

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

"use client"
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import {
  DndContext, DragOverlay, KeyboardSensor, PointerSensor, closestCenter, pointerWithin, useDroppable, useSensor, useSensors,
  type CollisionDetection, type DragEndEvent, type DragOverEvent, type DragStartEvent,
} from "@dnd-kit/core"
import { SortableContext, arrayMove, rectSortingStrategy, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { GripVertical } from "lucide-react"
import { cn } from "@/lib/utils"
import { useDashboardStore } from "@/stores/useDashboardStore"
import { widgetRegistry } from "@/lib/widget-registry"
import { layoutLines, rowSpan, spanOf, type CompactRows, type LayoutLine, type LayoutMode } from "@/lib/dashboard-layout"
import { ROW_COLUMNS, rowHeightPx, type WidgetPlacement } from "@/types/dashboard"
import { WidgetWrapper } from "./widget-wrapper"
import { WidgetDragHandleProvider } from "./drag-handle"

/** From this grid width the desktop rows are shown as stored and can be rearranged. */
const WIDE_MIN = 996
const TWO_MIN = 480
const FULL_HEIGHT: CompactRows = {}
/** Critically damped, no overshoot; matches the panel curve used across the shell. */
const SETTLE = { duration: 240, easing: "cubic-bezier(0.32, 0.72, 0, 1)" }

type Rows = WidgetPlacement[][]
const modeFor = (width: number): LayoutMode => width >= WIDE_MIN ? "wide" : width >= TWO_MIN ? "two" : "one"
const ROW = "row:"
const NEW_ROW = "new-row:"

function useElementWidth() {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState<number | null>(null)
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  return { ref, width }
}

/** Where an id points in the rows: a widget (row and index) or a row container (index -1). */
function locate(rows: Rows, id: string): { row: number; index: number } | null {
  if (id.startsWith(ROW)) return { row: Number(id.slice(ROW.length)), index: -1 }
  if (id.startsWith(NEW_ROW)) return null
  const row = rows.findIndex(r => r.some(w => w.instanceId === id))
  return row < 0 ? null : { row, index: rows[row].findIndex(w => w.instanceId === id) }
}

/** The pointer decides first, widgets win over their row; the keyboard falls back to the nearest target. */
const collision: CollisionDetection = args => {
  const hits = pointerWithin(args)
  if (hits.length === 0) return closestCenter(args)
  const widgets = hits.filter(hit => !String(hit.id).startsWith(ROW))
  return widgets.length > 0 ? widgets : hits
}

export function WidgetGrid() {
  const layout = useDashboardStore(s => s.layout)
  const isEditMode = useDashboardStore(s => s.isEditMode)
  const setRows = useDashboardStore(s => s.setRows)
  const { ref, width } = useElementWidth()
  const mode = width == null ? null : modeFor(width)
  const canMove = isEditMode && mode === "wide"

  // Calm widgets ask for fewer rows. While arranging, every widget shows its configured size.
  const [compactRows, setCompactRows] = useState<CompactRows>({})
  const reportCompactRows = useCallback((id: string, rows: number | null) => setCompactRows(prev => {
    if (rows == null) {
      if (!(id in prev)) return prev
      const next = { ...prev }
      delete next[id]
      return next
    }
    return prev[id] === rows ? prev : { ...prev, [id]: rows }
  }), [])

  // While dragging, rows live here so a widget can visit other rows before it is dropped.
  const [draft, setDraft] = useState<Rows | null>(null)
  const [active, setActive] = useState<WidgetPlacement | null>(null)
  const [overRow, setOverRow] = useState<number | null>(null)
  const rows = draft ?? layout.rows
  const lines = useMemo(() => mode ? layoutLines(rows, mode, isEditMode ? FULL_HEIGHT : compactRows) : [], [rows, mode, isEditMode, compactRows])

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  const onDragStart = ({ active: dragged }: DragStartEvent) => {
    setDraft(layout.rows)
    setActive(layout.rows.flat().find(w => w.instanceId === dragged.id) ?? null)
  }
  const onDragOver = ({ active: dragged, over }: DragOverEvent) => {
    if (!over || !draft) return
    const from = locate(draft, String(dragged.id))
    const to = locate(draft, String(over.id))
    setOverRow(to?.row ?? null)
    if (!from || !to || from.row === to.row) return
    const widget = draft[from.row][from.index]
    // A full row does not take the widget; the row shows why instead.
    if (rowSpan(draft[to.row]) + spanOf(widget.size) > ROW_COLUMNS) return
    const next = draft.map(row => row.filter(w => w.instanceId !== widget.instanceId))
    const at = to.index < 0 ? next[to.row].length : to.index
    next[to.row] = [...next[to.row].slice(0, at), widget, ...next[to.row].slice(at)]
    setDraft(next)
  }
  const finish = () => {
    setDraft(null)
    setActive(null)
    setOverRow(null)
  }
  const onDragEnd = ({ active: dragged, over }: DragEndEvent) => {
    const id = String(dragged.id)
    let next: Rows = draft ?? layout.rows
    const overId = over ? String(over.id) : null
    if (overId?.startsWith(NEW_ROW)) {
      const widget = next.flat().find(w => w.instanceId === id)
      if (widget) {
        next = next.map(row => row.filter(w => w.instanceId !== id))
        next.splice(Number(overId.slice(NEW_ROW.length)), 0, [widget])
      }
    } else if (overId) {
      const from = locate(next, id)
      const to = locate(next, overId)
      if (from && to && from.row === to.row && to.index >= 0 && from.index !== to.index) {
        next = next.map((row, i) => i === from.row ? arrayMove(row, from.index, to.index) : row)
      }
    }
    setRows(next)
    finish()
  }

  // One tree for viewing and arranging: entering edit mode only switches dragging on,
  // so widgets keep their state and data instead of mounting again.
  return <div ref={ref}>
    {isEditMode && mode && !canMove && <p className="mb-3 text-xs text-muted-foreground">Перемещение доступно при ширине области от {WIDE_MIN} px. Состав, размеры и настройки можно менять здесь.</p>}
    {mode && <DndContext sensors={sensors} collisionDetection={collision} onDragStart={onDragStart} onDragOver={onDragOver} onDragEnd={onDragEnd} onDragCancel={finish}
      accessibility={{ screenReaderInstructions: { draggable: "Нажмите пробел, чтобы взять виджет. Стрелки переносят его, пробел кладёт, Esc отменяет." } }}>
      <div className="flex flex-col gap-3">
        {lines.map(line => {
          const blocked = !!active && overRow === line.row && !rows[line.row].some(w => w.instanceId === active.instanceId)
            && rowSpan(rows[line.row]) + spanOf(active.size) > ROW_COLUMNS
          return <div key={line.key} className="flex flex-col gap-3">
            {canMove && <NewRowSlot id={`${NEW_ROW}${line.row}`} dragging={!!active} />}
            <SortableContext items={line.cells.map(c => c.widget.instanceId)} strategy={rectSortingStrategy} disabled={!canMove}>
              <DroppableLine line={line} blocked={blocked} disabled={!canMove}>
                {line.cells.map(c => <SortableCell key={c.widget.instanceId} id={c.widget.instanceId} span={c.span} movable={canMove}
                  label={widgetRegistry.get(c.widget.widgetType)?.label ?? c.widget.widgetType}>
                  <WidgetWrapper placement={c.widget} isEditMode={isEditMode} onCompactRows={reportCompactRows} />
                </SortableCell>)}
              </DroppableLine>
            </SortableContext>
          </div>
        })}
        {canMove && <NewRowSlot id={`${NEW_ROW}${rows.length}`} dragging={!!active} />}
      </div>
      <DragOverlay dropAnimation={SETTLE}>{active && <DragPreview widget={active} />}</DragOverlay>
    </DndContext>}
  </div>
}

/**
 * One line on screen: a grid as tall as its widgets need, settling smoothly when that changes.
 * Its one track is held to the line's height, so content taller than the widget scrolls inside
 * the widget instead of stretching the track and running under the next line.
 */
function Line({ line, children, className, nodeRef }: { line: LayoutLine; children: ReactNode; className?: string; nodeRef?: (element: HTMLElement | null) => void }) {
  return <div ref={nodeRef} className={cn("relative grid gap-3 transition-[height] duration-[240ms] ease-panel motion-reduce:transition-none", className)}
    style={{ gridTemplateColumns: `repeat(${line.columns}, minmax(0, 1fr))`, gridTemplateRows: "minmax(0, 1fr)", height: rowHeightPx(line.height) }}>
    {children}
  </div>
}

function DroppableLine({ line, blocked, disabled, children }: { line: LayoutLine; blocked: boolean; disabled: boolean; children: ReactNode }) {
  const { setNodeRef } = useDroppable({ id: `${ROW}${line.row}`, disabled })
  return <Line line={line} nodeRef={setNodeRef} className={cn("rounded-xl outline-offset-4", blocked && "outline outline-2 outline-destructive/60")}>
    {children}
    {blocked && <span className="pointer-events-none absolute -top-2.5 right-3 z-10 rounded-md border border-destructive/30 bg-card px-1.5 py-0.5 text-[11px] font-medium text-destructive shadow-control">Ряд заполнен</span>}
  </Line>
}

function SortableCell({ id, span, label, movable, children }: { id: string; span: number; label: string; movable: boolean; children: ReactNode }) {
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, transform, transition, isDragging } = useSortable({ id, transition: SETTLE, disabled: !movable })
  const handle = <button type="button" ref={setActivatorNodeRef} {...attributes} {...listeners} aria-label={`Переместить: ${label}`}
    className="drag-handle flex min-w-0 cursor-grab touch-none items-center gap-1 rounded-sm px-1 text-xs text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/25 active:cursor-grabbing">
    <GripVertical className="size-4 shrink-0" /><span className="truncate">{label}</span>
  </button>
  return <div ref={setNodeRef} className={cn("min-w-0 rounded-xl", isDragging && "opacity-40 outline-dashed outline-2 outline-border")}
    style={{ gridColumn: `span ${span} / span ${span}`, transform: CSS.Translate.toString(transform), transition }}>
    <WidgetDragHandleProvider value={movable ? handle : null}>{children}</WidgetDragHandleProvider>
  </div>
}

/** A drop target above each row and below the last one: dropping here starts a new row. */
function NewRowSlot({ id, dragging }: { id: string; dragging: boolean }) {
  const { setNodeRef, isOver } = useDroppable({ id })
  return <div ref={setNodeRef} aria-hidden={!dragging}
    className={cn("flex h-7 items-center justify-center rounded-lg border border-dashed text-xs transition-colors",
      isOver ? "border-info bg-info/10 text-info" : dragging ? "border-input text-muted-foreground" : "border-transparent text-transparent")}>
    Новый ряд
  </div>
}

function DragPreview({ widget }: { widget: WidgetPlacement }) {
  const definition = widgetRegistry.get(widget.widgetType)
  const Icon = definition?.icon
  return <div className="flex w-60 cursor-grabbing items-center gap-2 rounded-lg border bg-popover px-3 py-2 text-sm font-medium shadow-floating">
    <GripVertical className="size-4 text-muted-foreground" />
    {Icon && <Icon className="size-4 text-muted-foreground" />}
    <span className="truncate">{definition?.label ?? widget.widgetType}</span>
  </div>
}

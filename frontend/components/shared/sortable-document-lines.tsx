// components/shared/sortable-document-lines.tsx
//
// Generic DnD infrastructure for document table part rows.
// Split into Provider (wraps <table>) and Body (<tbody>) to avoid
// hydration error: DndContext renders hidden <div> for accessibility,
// which cannot be a child of <table>.

"use client"

import React, { useCallback, useMemo } from "react"
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core"
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { GripVertical } from "lucide-react"
import type { FormLine } from "@/lib/document-form"

// ── Types ───────────────────────────────────────────────────────────────

export interface DragHandleProps {
  /** Spread onto the drag handle button element */
  attributes: React.HTMLAttributes<HTMLButtonElement>
  listeners: Record<string, Function> | undefined
}

export interface SortableRowRenderProps {
  line: FormLine
  index: number
  /** Ref to attach to the <tr> element */
  setNodeRef: (node: HTMLElement | null) => void
  /** Style to apply to the <tr> for transform/transition */
  style: React.CSSProperties
  /** Props to spread onto the drag handle button */
  dragHandleProps: DragHandleProps
  /** Whether this row is currently being dragged */
  isDragging: boolean
}

// ── Sortable Row Wrapper ────────────────────────────────────────────────

function SortableRow({
  line,
  index,
  renderRow,
}: {
  line: FormLine
  index: number
  renderRow: (props: SortableRowRenderProps) => React.ReactNode
}) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: line._key })

  const style: React.CSSProperties = useMemo(
    () => ({
      transform: CSS.Transform.toString(transform),
      transition,
      zIndex: isDragging ? 50 : undefined,
      position: isDragging ? ("relative" as const) : undefined,
      opacity: isDragging ? 0.85 : undefined,
    }),
    [transform, transition, isDragging],
  )

  return (
    <>
      {renderRow({
        line,
        index,
        setNodeRef,
        style,
        dragHandleProps: { attributes, listeners },
        isDragging,
      })}
    </>
  )
}

// ── Drag Handle Button (shared visual) ──────────────────────────────────

export function DragHandleButton({
  dragHandleProps,
  compact,
}: {
  dragHandleProps: DragHandleProps
  compact?: boolean
}) {
  return (
    <button
      type="button"
      className={`inline-flex items-center justify-center shrink-0 cursor-grab rounded
        text-muted-foreground/0 group-hover:text-muted-foreground/50
        hover:!text-muted-foreground active:cursor-grabbing
        transition-colors touch-none ${compact ? "size-4" : "size-5"}`}
      {...dragHandleProps.attributes}
      {...(dragHandleProps.listeners as React.HTMLAttributes<HTMLButtonElement>)}
      tabIndex={-1}
    >
      <GripVertical className={compact ? "size-3" : "size-3.5"} />
    </button>
  )
}

// ── Provider: wraps OUTSIDE <table> to avoid <div> in <table> ───────────
//
// DndContext renders hidden <div> elements for accessibility (HiddenText,
// LiveRegion). These CANNOT be children of <table>. So DndContext must
// wrap the <table>, not be inside it.

interface DocumentLinesDndProviderProps {
  /** Lines array — used to resolve drag IDs to indices */
  items: FormLine[]
  /** Called when drag completes with (oldIndex, newIndex) */
  onReorder: (oldIndex: number, newIndex: number) => void
  children: React.ReactNode
}

export function DocumentLinesDndProvider({
  items,
  onReorder,
  children,
}: DocumentLinesDndProviderProps) {
  const sortableIds = useMemo(() => items.map((l) => l._key), [items])

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 5 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  )

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event
      if (!over || active.id === over.id) return

      const oldIndex = sortableIds.indexOf(Number(active.id))
      const newIndex = sortableIds.indexOf(Number(over.id))
      if (oldIndex === -1 || newIndex === -1) return

      onReorder(oldIndex, newIndex)
    },
    [sortableIds, onReorder],
  )

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragEnd={handleDragEnd}
    >
      {children}
    </DndContext>
  )
}

// ── Body: renders SortableContext + <tbody> INSIDE <table> ───────────────

interface SortableDocumentLinesBodyProps {
  /** Lines array — used for sortable IDs (via _key) */
  items: FormLine[]
  /** Render callback for each row */
  renderRow: (props: SortableRowRenderProps) => React.ReactNode
  /** Empty state content (when items.length === 0) */
  emptyContent?: React.ReactNode
}

export function SortableDocumentLinesBody({
  items,
  renderRow,
  emptyContent,
}: SortableDocumentLinesBodyProps) {
  const sortableIds = useMemo(() => items.map((l) => l._key), [items])

  if (items.length === 0) {
    return <tbody>{emptyContent}</tbody>
  }

  return (
    <SortableContext
      items={sortableIds}
      strategy={verticalListSortingStrategy}
    >
      <tbody>
        {items.map((line, idx) => (
          <SortableRow
            key={line._key}
            line={line}
            index={idx}
            renderRow={renderRow}
          />
        ))}
      </tbody>
    </SortableContext>
  )
}

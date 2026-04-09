// components/shared/column-chooser-popover.tsx — Dialog UI for toggling column visibility
// and reordering columns via drag-and-drop.
// Uses @dnd-kit for accessible, keyboard-friendly sortable lists.

"use client"

import React, { useMemo, useState, useCallback } from "react"
import { Columns3, GripVertical, RotateCcw } from "lucide-react"
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
    arrayMove,
    SortableContext,
    sortableKeyboardCoordinates,
    useSortable,
    verticalListSortingStrategy,
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Checkbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import type { Column } from "@/components/shared/data-table"

// ── Types ───────────────────────────────────────────────────────────────

interface ColumnChooserPopoverProps<T> {
    /** All columns ordered for display: visible first (in order), then hidden. */
    allColumns: Column<T>[]
    /** Keys of currently visible columns (in display order). */
    visibleKeys: string[]
    /** Toggle a column by key. */
    onToggle: (key: string) => void
    /** Reorder visible columns. Receives new ordered visible keys. */
    onReorder?: (newVisibleKeys: string[]) => void
    /** Reset to default column set. */
    onReset: () => void
    /** Controlled open state. */
    open?: boolean
    /** Callback when open state changes. */
    onOpenChange?: (open: boolean) => void
    /** Trigger element (unused — kept for API compat, Dialog is opened via open prop). */
    children?: React.ReactNode
}

// ── Sortable Item ───────────────────────────────────────────────────────

interface SortableColumnItemProps {
    id: string
    label: string
    checked: boolean
    onToggle: () => void
}

function SortableColumnItem({ id, label, checked, onToggle }: SortableColumnItemProps) {
    const {
        attributes,
        listeners,
        setNodeRef,
        transform,
        transition,
        isDragging,
    } = useSortable({ id })

    const style: React.CSSProperties = {
        transform: CSS.Transform.toString(transform),
        transition,
        zIndex: isDragging ? 50 : undefined,
        position: isDragging ? "relative" : undefined,
    }

    return (
        <div
            ref={setNodeRef}
            style={style}
            className={`flex items-center gap-1 px-2 py-1.5 cursor-default transition-colors ${
                isDragging
                    ? "bg-accent shadow-md rounded-md opacity-90"
                    : "hover:bg-accent/50"
            }`}
        >
            {/* Drag handle */}
            <button
                type="button"
                className="flex items-center justify-center shrink-0 size-6 cursor-grab rounded hover:bg-accent active:cursor-grabbing text-muted-foreground/50 hover:text-muted-foreground"
                {...attributes}
                {...listeners}
            >
                <GripVertical className="size-3.5" />
            </button>

            {/* Checkbox + label */}
            <label className="flex items-center gap-2 flex-1 cursor-pointer min-w-0">
                <Checkbox
                    checked={checked}
                    onCheckedChange={onToggle}
                    className="pointer-events-none shrink-0"
                    tabIndex={-1}
                />
                <span className="text-sm select-none truncate">{label}</span>
            </label>
        </div>
    )
}

// ── Component ───────────────────────────────────────────────────────────

export function ColumnChooserPopover<T>({
    allColumns,
    visibleKeys,
    onToggle,
    onReorder,
    onReset,
    open,
    onOpenChange,
}: ColumnChooserPopoverProps<T>) {
    const visibleSet = useMemo(() => new Set(visibleKeys), [visibleKeys])

    // Maintain internal display order: visible first (in order), then hidden.
    // This gets reset whenever the dialog data changes externally.
    const orderedKeys = useMemo(
        () => allColumns.map((c) => c.key),
        [allColumns],
    )

    // Column map for quick lookups.
    const colMap = useMemo(
        () => new Map(allColumns.map((c) => [c.key, c])),
        [allColumns],
    )

    // Track local ordering state for smooth DnD within dialog session.
    const [localOrder, setLocalOrder] = useState<string[] | null>(null)

    // When dialog opens/closes, reset local order.
    const handleOpenChange = useCallback(
        (isOpen: boolean) => {
            if (!isOpen) setLocalOrder(null)
            onOpenChange?.(isOpen)
        },
        [onOpenChange],
    )

    const displayOrder = localOrder ?? orderedKeys

    // ── DnD sensors ─────────────────────────────────────────────────────
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

            const oldIndex = displayOrder.indexOf(String(active.id))
            const newIndex = displayOrder.indexOf(String(over.id))
            if (oldIndex === -1 || newIndex === -1) return

            const newOrder = arrayMove(displayOrder, oldIndex, newIndex)
            setLocalOrder(newOrder)

            // Extract visible keys in new order and persist.
            if (onReorder) {
                const newVisibleKeys = newOrder.filter((key) => visibleSet.has(key))
                onReorder(newVisibleKeys)
            }
        },
        [displayOrder, visibleSet, onReorder],
    )

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent className="sm:max-w-[360px] p-0 gap-0">
                <DialogHeader className="px-4 py-3 border-b">
                    <DialogTitle className="flex items-center gap-2 text-sm font-medium">
                        <Columns3 className="h-4 w-4 text-muted-foreground" />
                        Настройка колонок
                    </DialogTitle>
                </DialogHeader>

                {/* Column list with DnD */}
                <div className="max-h-[360px] overflow-y-auto py-1">
                    <DndContext
                        sensors={sensors}
                        collisionDetection={closestCenter}
                        onDragEnd={handleDragEnd}
                    >
                        <SortableContext
                            items={displayOrder}
                            strategy={verticalListSortingStrategy}
                        >
                            {displayOrder.map((key) => {
                                const col = colMap.get(key)
                                if (!col) return null
                                return (
                                    <SortableColumnItem
                                        key={key}
                                        id={key}
                                        label={col.label}
                                        checked={visibleSet.has(key)}
                                        onToggle={() => onToggle(key)}
                                    />
                                )
                            })}
                        </SortableContext>
                    </DndContext>
                </div>

                {/* Footer — reset button */}
                <Separator />
                <div className="px-4 py-2.5">
                    <Button
                        variant="ghost"
                        size="sm"
                        className="w-full justify-start gap-2 text-muted-foreground"
                        onClick={() => {
                            setLocalOrder(null)
                            onReset()
                        }}
                    >
                        <RotateCcw className="h-3.5 w-3.5" />
                        Сбросить по умолчанию
                    </Button>
                </div>
            </DialogContent>
        </Dialog>
    )
}

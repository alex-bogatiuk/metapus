"use client"

import React, { useMemo } from "react"
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { cn } from "@/lib/utils"
import {
    ContextMenu,
    ContextMenuTrigger,
    ContextMenuContent,
} from "@/components/ui/context-menu"

// ── Types ───────────────────────────────────────────────────────────────

export interface Column<T> {
    /** Unique key matching a field in T (used for sorting). */
    key: string
    /** Column header label. */
    label: string
    /** Text alignment. Default = 'left'. */
    align?: "left" | "right" | "center"
    /** Whether column is sortable. Default = false. */
    sortable?: boolean
    /** Custom cell renderer. Falls back to `String(item[key])`. */
    render?: (item: T) => React.ReactNode
    /** Extra className applied to both th and td. */
    className?: string
    /** Default width in pixels. Used as initial width for resize. */
    width?: number
    /** Minimum width in pixels for resize. Default = 60. */
    minWidth?: number
}

export interface DataTableProps<T extends { id: string }> {
    /** Items to display. */
    data: T[]
    /** Column definitions. */
    columns: Column<T>[]

    // ── Selection (from useListSelection) ──────────────────────────────
    selectedIds: string[]
    isAllSelected: boolean
    isIndeterminate: boolean
    onToggleAll: () => void
    onToggleItem: (id: string, shiftKey: boolean) => void

    // ── Sorting (controlled, from useUrlSort) ─────────────────────────
    /** Currently sorted column key. */
    sortColumn?: string | null
    /** Current sort direction. */
    sortDirection?: "asc" | "desc"
    /** Called when user clicks a sortable column header. */
    onSort?: (key: string) => void

    // ── Row actions ────────────────────────────────────────────────────
    /** Called on single-click of a row (focus / preview). */
    onRowClick?: (item: T) => void
    /** Called on double-click of a row. */
    onRowDoubleClick?: (item: T) => void
    /** ID of the currently focused (previewed) row. */
    focusedId?: string | null

    // ── Optional prefix column (e.g. status icon) ─────────────────────
    renderPrefix?: (item: T) => React.ReactNode

    // ── Optional context menu (right-click) ──────────────────────────
    /** When provided, right-clicking a row shows a custom context menu
     *  instead of the browser default. Return ContextMenuItem nodes.
     *  @param item — the row that was right-clicked
     *  @param targets — effective target items: all selected items when the
     *                   right-clicked row is part of the selection, otherwise
     *                   just [item] (1C / Windows Explorer pattern). */
    renderContextMenu?: (item: T, targets: T[]) => React.ReactNode

    // ── Optional per-row className (e.g. deletion mark styling) ─────
    /** When provided, the returned className is applied to the row <tr>. */
    rowClassName?: (item: T) => string | undefined

    // ── Column resize ─────────────────────────────────────────────────
    /** Computed pixel widths per column (from useColumnResize). */
    colWidths?: number[]
    /** Callback when user starts dragging a column resize handle. */
    onResizeStart?: (colIndex: number, e: React.MouseEvent) => void
    /** True while a resize is in progress (disables text selection). */
    isResizing?: boolean
}

// ── Component ───────────────────────────────────────────────────────────

export function DataTable<T extends { id: string }>({
    data,
    columns,
    selectedIds,
    isAllSelected,
    isIndeterminate,
    onToggleAll,
    onToggleItem,
    sortColumn = null,
    sortDirection = "asc",
    onSort,
    onRowClick,
    onRowDoubleClick,
    focusedId = null,
    renderPrefix,
    renderContextMenu,
    rowClassName,
    colWidths,
    onResizeStart,
    isResizing,
}: DataTableProps<T>) {

    // ⚡ Perf: O(1) selection lookup via Set instead of O(N) Array.includes() per row.
    // Before: N × includes() = O(N²). After: 1 Set build + N × has() = O(N).
    const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds])

    // Determine the checked state for the header checkbox
    const headerChecked = isAllSelected
        ? true
        : isIndeterminate
            ? ("indeterminate" as const)
            : false

    const alignClass = (align?: "left" | "right" | "center") => {
        if (align === "right") return "text-right"
        if (align === "center") return "text-center"
        return "text-left"
    }

    const SortIcon = ({ colKey }: { colKey: string }) => {
        if (sortColumn !== colKey) {
            return <ArrowUpDown className="ml-1 inline h-3 w-3 shrink-0 opacity-0 group-hover:opacity-40" />
        }
        return sortDirection === "asc" ? (
            <ArrowUp className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
        ) : (
            <ArrowDown className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
        )
    }

    return (
        <table
            className={cn(
                "w-full text-sm table-fixed",
                isResizing && "select-none"
            )}
        >
            {/* Colgroup — controls column widths for resize */}
            <colgroup>
                {/* Checkbox column */}
                <col style={{ width: 40 }} />
                {/* Optional prefix column */}
                {renderPrefix && <col style={{ width: 32 }} />}
                {/* Data columns */}
                {columns.map((col, i) => (
                    <col
                        key={col.key}
                        style={{
                            width: colWidths?.[i] ?? col.width ?? undefined,
                        }}
                    />
                ))}
            </colgroup>

            <thead className="sticky top-0 z-10">
                <tr className="border-b bg-muted/70">
                    {/* Checkbox column — fixed width, centered */}
                    <th className="w-[40px] min-w-[40px] max-w-[40px] px-0 py-2">
                        <div className="flex items-center justify-center">
                            <Checkbox
                                checked={headerChecked}
                                onCheckedChange={onToggleAll}
                            />
                        </div>
                    </th>

                    {/* Optional prefix column (status icon, etc.) */}
                    {renderPrefix && (
                        <th className="w-[32px] min-w-[32px] max-w-[32px] px-1 py-2" />
                    )}

                    {/* Data columns with resize handles */}
                    {columns.map((col, colIndex) => (
                        <th
                            key={col.key}
                            className={cn(
                                "relative px-4 py-2 text-xs font-medium text-muted-foreground select-none",
                                alignClass(col.align),
                                col.sortable && "cursor-pointer group hover:text-foreground transition-colors",
                                col.className
                            )}
                            onClick={col.sortable && onSort ? () => { if (!isResizing) onSort(col.key) } : undefined}
                        >
                            <div className="truncate">
                                {col.label}
                                {col.sortable && <SortIcon colKey={col.key} />}
                            </div>

                            {/* Resize handle */}
                            {onResizeStart && (
                                <div
                                    className="absolute right-0 top-0 h-full w-[5px] cursor-col-resize z-20 group/resize hover:bg-primary/30 active:bg-primary/50"
                                    onMouseDown={(e) => onResizeStart(colIndex, e)}
                                    onClick={(e) => e.stopPropagation()}
                                >
                                    <div className="absolute right-0 top-1/4 h-1/2 w-[1px] bg-border group-hover/resize:bg-primary/60" />
                                </div>
                            )}
                        </th>
                    ))}
                </tr>
            </thead>
            <tbody>
                {data.map((item) => {
                    const isSelected = selectedSet.has(item.id)

                    const isFocused = focusedId === item.id

                    const row = (
                        <tr
                            key={item.id}
                            className={cn(
                                "border-b transition-colors cursor-pointer h-9",
                                isFocused
                                    ? "bg-primary/15 ring-1 ring-inset ring-primary/30"
                                    : isSelected
                                        ? "bg-primary/10"
                                        : "hover:bg-primary/5",
                                rowClassName?.(item)
                            )}
                            onClick={() => onRowClick?.(item)}
                            onDoubleClick={() => onRowDoubleClick?.(item)}
                        >
                            {/* Checkbox cell — same fixed width as header, centered */}
                            <td className="w-[40px] min-w-[40px] max-w-[40px] px-0 py-0">
                                <div className="flex items-center justify-center h-9">
                                    <Checkbox
                                        checked={isSelected}
                                        onClick={(e: React.MouseEvent) => {
                                            e.stopPropagation()
                                            onToggleItem(item.id, e.shiftKey)
                                        }}
                                        onCheckedChange={() => {
                                            /* handled by onClick to capture shiftKey */
                                        }}
                                    />
                                </div>
                            </td>

                            {/* Optional prefix cell */}
                            {renderPrefix && (
                                <td className="w-[32px] min-w-[32px] max-w-[32px] px-1 py-0">
                                    <div className="flex items-center h-9">
                                        {renderPrefix(item)}
                                    </div>
                                </td>
                            )}

                            {/* Data cells — fixed height, horizontally clipped */}
                            {columns.map((col) => (
                                <td
                                    key={col.key}
                                    className={cn(
                                        "px-4 py-0 h-9 max-h-9",
                                        alignClass(col.align),
                                        col.className
                                    )}
                                >
                                    <div className="flex items-center h-9 min-w-0 whitespace-nowrap overflow-hidden text-ellipsis">
                                        {col.render
                                            ? col.render(item)
                                            : String((item as Record<string, unknown>)[col.key] ?? "")}
                                    </div>
                                </td>
                            ))}
                        </tr>
                    )

                    if (renderContextMenu) {
                        // 1C / Windows Explorer pattern:
                        // If the right-clicked row is part of the current
                        // selection, all selected rows become the targets;
                        // otherwise only the clicked row is targeted.
                        const targets = selectedSet.has(item.id)
                            ? data.filter((d) => selectedSet.has(d.id))
                            : [item]

                        return (
                            <ContextMenu key={item.id}>
                                <ContextMenuTrigger asChild>
                                    {row}
                                </ContextMenuTrigger>
                                <ContextMenuContent className="w-56">
                                    {renderContextMenu(item, targets)}
                                </ContextMenuContent>
                            </ContextMenu>
                        )
                    }

                    return row
                })}
            </tbody>
        </table>
    )
}

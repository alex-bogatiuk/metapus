"use client"

import type { JsonObject } from "@/lib/json-value"

import React, { useEffect, useMemo, useRef, useState } from "react"
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { cn } from "@/lib/utils"
import { summarize, type SummaryOperand } from "@/lib/selection-summary"
import { SelectionSummaryPopover } from "@/components/shared/selection-summary-popover"
import { useCompactMode } from "@/hooks/useCompactMode"
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
    /** Canonical filter field key (e.g. networkId for a `network` display column). */
    filterKey?: string
    /** Number of a row for selection totals; absent for non-numeric columns. */
    summary?: (item: T) => SummaryOperand | null
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
    /** Called on single-click of a row (current-row focus / preview). */
    onRowClick?: (item: T) => void
    /** Called on click of a data cell. Sets the current column for Alt+F. */
    onCellClick?: (item: T, columnKey: string) => void
    /**
     * Row selection by cell clicks (1C style): Shift+click selects the rows
     * from the anchor (the current row before the first Shift+click) to the
     * clicked one ("replace"); Cmd/Ctrl+click toggles the clicked row.
     * With the current row selected, a numeric current column shows totals.
     */
    onSelectRows?: (ids: string[], mode: "replace" | "toggle") => void
    /** Called on double-click of a row. */
    onRowDoubleClick?: (item: T) => void
    /** ID of the current row. Independent of checkbox selection. */
    focusedId?: string | null
    /** Key of the current column (the cell last clicked in the current row). */
    focusedColumnKey?: string | null

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

/** Marks the full value a cell copies when its visible text is shortened. */
export const COPY_VALUE_ATTR = "data-copy-value"

const EMPTY_CELL_TEXT = "—"

function isEditable(el: Element | null): boolean {
    if (!el) return false
    if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement || el instanceof HTMLSelectElement) return true
    return el instanceof HTMLElement && el.isContentEditable
}

/** Text a cell puts on the clipboard: an explicit full value, else what it shows. */
export function cellCopyText(cell: HTMLElement): string {
    const marked = cell.matches(`[${COPY_VALUE_ATTR}]`)
        ? cell
        : cell.querySelector<HTMLElement>(`[${COPY_VALUE_ATTR}]`)
    const text = marked
        ? marked.getAttribute(COPY_VALUE_ATTR) ?? ""
        : (cell.innerText || cell.textContent || "").trim()
    return text === EMPTY_CELL_TEXT ? "" : text
}

function SortIcon({
    colKey,
    sortColumn,
    sortDirection,
}: {
    colKey: string
    sortColumn: string | null
    sortDirection: "asc" | "desc"
}) {
    if (sortColumn !== colKey) {
        return <ArrowUpDown className="ml-1 inline h-3 w-3 shrink-0 opacity-0 group-hover:opacity-40" />
    }
    return sortDirection === "asc" ? (
        <ArrowUp className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
    ) : (
        <ArrowDown className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
    )
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
    onCellClick,
    onSelectRows,
    onRowDoubleClick,
    focusedId = null,
    focusedColumnKey = null,
    renderPrefix,
    renderContextMenu,
    rowClassName,
    colWidths,
    onResizeStart,
    isResizing,
}: DataTableProps<T>) {

    const compact = useCompactMode()
    const rowH = compact ? "h-7" : "h-9"
    const cellPx = compact ? "px-2" : "px-4"
    const thPy = compact ? "py-1" : "py-2"
    const fontSize = compact ? "text-xs" : "text-sm"

    // O(1) selection lookup via Set instead of O(N) Array.includes() per row.
    const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds])

    // Anchor of Shift+click ranges: the row clicked last without Shift.
    const anchorRef = useRef<string | null>(null)
    const handleCellClick = (item: T, columnKey: string, event: React.MouseEvent) => {
        if (event.shiftKey && onSelectRows) {
            const anchor = anchorRef.current ?? focusedId ?? item.id
            const from = data.findIndex((row) => row.id === anchor)
            const to = data.findIndex((row) => row.id === item.id)
            if (from !== -1 && to !== -1) {
                onSelectRows(data.slice(Math.min(from, to), Math.max(from, to) + 1).map((row) => row.id), "replace")
            }
            anchorRef.current = anchor
        } else {
            if ((event.metaKey || event.ctrlKey) && onSelectRows) onSelectRows([item.id], "toggle")
            anchorRef.current = item.id
        }
        onCellClick?.(item, columnKey)
    }

    // Totals of the current column over the selection that holds the current row.
    const summaryColumn = columns.find((col) => col.key === focusedColumnKey)
    const summary = useMemo(() => {
        if (!summaryColumn?.summary || !focusedId || !selectedSet.has(focusedId)) return null
        const rows = data.filter((row) => selectedSet.has(row.id))
        if (rows.length < 2) return null
        return summarize(rows.map((row) => summaryColumn.summary!(row)))
    }, [summaryColumn, focusedId, selectedSet, data])
    const summaryKey = summary ? `${focusedColumnKey}|${selectedIds.length}|${focusedId}` : ""
    const [dismissedSummary, setDismissedSummary] = useState("")

    // 1C-style copy: Cmd/Ctrl+C (or Edit → Copy) with no text selected puts
    // the current cell's value on the clipboard. A real text selection,
    // focus in an editor, or focus outside this table keeps the native copy.
    const tableRef = useRef<HTMLTableElement>(null)
    useEffect(() => {
        const currentCellText = (): string | null => {
            const table = tableRef.current
            if (!table || table.offsetParent === null) return null
            const selection = document.getSelection()
            if (selection && !selection.isCollapsed && selection.toString() !== "") return null
            const active = document.activeElement
            if (isEditable(active)) return null
            if (active && active !== document.body && !table.contains(active)) return null
            const cell = table.querySelector<HTMLElement>('td[data-current-cell="true"]')
            return cell ? cellCopyText(cell) : null
        }
        // Keyboard path: Safari fires no `copy` event when nothing is
        // selected, so the shortcut writes through the Clipboard API.
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.defaultPrevented || event.altKey || event.shiftKey) return
            if (!(event.metaKey || event.ctrlKey) || event.code !== "KeyC") return
            if (!navigator.clipboard?.writeText) return
            const text = currentCellText()
            if (text === null) return
            event.preventDefault()
            void navigator.clipboard.writeText(text).catch(() => {})
        }
        // Menu path (Edit → Copy) and browsers without the Clipboard API.
        const onCopy = (event: ClipboardEvent) => {
            if (event.defaultPrevented || !event.clipboardData) return
            const text = currentCellText()
            if (text === null) return
            event.clipboardData.setData("text/plain", text)
            event.preventDefault()
        }
        document.addEventListener("keydown", onKeyDown)
        document.addEventListener("copy", onCopy)
        return () => {
            document.removeEventListener("keydown", onKeyDown)
            document.removeEventListener("copy", onCopy)
        }
    }, [])

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

    return (
        <table
            ref={tableRef}
            className={cn(
                "w-full table-fixed",
                fontSize,
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
                <tr className="border-b border-border/70 bg-muted/85 backdrop-blur-sm">
                    {/* Checkbox column — fixed width, centered */}
                    <th className={cn("w-[40px] min-w-[40px] max-w-[40px] px-0", thPy)}>
                        <div className="flex items-center justify-center">
                            <Checkbox
                                checked={headerChecked}
                                onCheckedChange={onToggleAll}
                            />
                        </div>
                    </th>

                    {/* Optional prefix column (status icon, etc.) */}
                    {renderPrefix && (
                        <th className={cn("w-[32px] min-w-[32px] max-w-[32px] px-1", thPy)} />
                    )}

                    {/* Data columns with resize handles */}
                    {columns.map((col, colIndex) => (
                        <th
                            key={col.key}
                            data-column-key={col.key}
                            data-focused-column={focusedColumnKey === col.key ? "true" : undefined}
                            className={cn(
                                "relative text-xs font-medium text-muted-foreground select-none",
                                cellPx, thPy,
                                alignClass(col.align),
                                col.sortable && "cursor-pointer group hover:text-foreground transition-colors",
                                focusedColumnKey === col.key && "text-foreground",
                                col.className
                            )}
                            onClick={col.sortable && onSort ? () => { if (!isResizing) onSort(col.key) } : undefined}
                        >
                            <div className="truncate">
                                {col.label}
                                {col.sortable && (
                                    <SortIcon
                                        colKey={col.key}
                                        sortColumn={sortColumn}
                                        sortDirection={sortDirection}
                                    />
                                )}
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
            <tbody className="bg-workspace-surface">
                {data.map((item) => {
                    const isSelected = selectedSet.has(item.id)

                    const isFocused = focusedId === item.id
                    const hasSelectionIndicator = isFocused || isSelected

                    const row = (
                        <tr
                            key={item.id}
                            data-row-id={item.id}
                            tabIndex={-1}
                            data-focused={isFocused ? "true" : undefined}
                            data-selected={isSelected ? "true" : undefined}
                            aria-current={isFocused ? "true" : undefined}
                            aria-selected={isSelected}
                            className={cn(
                                "cursor-pointer border-b border-border/60 transition-colors",
                                rowH,
                                hasSelectionIndicator && "shadow-[inset_2px_0_0_hsl(var(--ring))]",
                                isFocused
                                    ? "bg-accent ring-1 ring-inset ring-ring/55"
                                    : isSelected
                                        ? "bg-accent/85"
                                        : "hover:bg-accent/65",
                                rowClassName?.(item)
                            )}
                            onClick={() => onRowClick?.(item)}
                            onDoubleClick={() => onRowDoubleClick?.(item)}
                        >
                            {/* Checkbox cell — same fixed width as header, centered */}
                            <td className="w-[40px] min-w-[40px] max-w-[40px] px-0 py-0">
                                <div className={cn("flex items-center justify-center", rowH)}>
                                    <Checkbox
                                        checked={isSelected}
                                        tabIndex={-1}
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
                                    <div className={cn("flex items-center", rowH)}>
                                        {renderPrefix(item)}
                                    </div>
                                </td>
                            )}

                            {/* Data cells — fixed height, horizontally clipped */}
                            {columns.map((col) => {
                                const isCurrentCell = isFocused && focusedColumnKey === col.key
                                const cell = (
                                <td
                                    key={col.key}
                                    data-column-key={col.key}
                                    data-current-cell={isCurrentCell ? "true" : undefined}
                                    className={cn(
                                        "py-0",
                                        cellPx, rowH,
                                        `max-${rowH}`,
                                        alignClass(col.align),
                                        isCurrentCell && "relative z-[1] shadow-[inset_0_0_0_1px_hsl(var(--ring))]",
                                        col.className
                                    )}
                                    onMouseDown={(event) => { if (event.shiftKey && onSelectRows) event.preventDefault() }}
                                    onClick={(event) => handleCellClick(item, col.key, event)}
                                >
                                    <div className={cn("flex items-center min-w-0 whitespace-nowrap overflow-hidden text-ellipsis", rowH,
                                        col.align === "right" && "justify-end", col.align === "center" && "justify-center")}>
                                        {col.render
                                            ? col.render(item)
                                            : String((item as JsonObject)[col.key] ?? "")}
                                    </div>
                                </td>
                                )
                                return isCurrentCell && summary && dismissedSummary !== summaryKey ? (
                                    <SelectionSummaryPopover key={col.key} summary={summary} label={col.label} onDismiss={() => setDismissedSummary(summaryKey)}>
                                        {cell}
                                    </SelectionSummaryPopover>
                                ) : cell
                            })}
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

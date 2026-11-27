"use client"

import type { JsonObject } from "@/lib/json-value"

import React, { useEffect, useMemo, useRef, useState } from "react"
import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react"
import { Checkbox } from "@/components/ui/checkbox"
import { cn } from "@/lib/utils"
import { summarize, type SummaryOperand } from "@/lib/selection-summary"
import { rowMoveTarget, rowRange } from "@/lib/row-selection"
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
        return <ArrowUpDown aria-hidden="true" className="ml-1 inline h-3 w-3 shrink-0 opacity-0 group-hover:opacity-40 group-focus-visible:opacity-40" />
    }
    return sortDirection === "asc" ? (
        <ArrowUp aria-hidden="true" className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
    ) : (
        <ArrowDown aria-hidden="true" className="ml-1 inline h-3 w-3 shrink-0 text-primary" />
    )
}

function alignClass(align?: "left" | "right" | "center"): string {
    if (align === "right") return "text-right"
    if (align === "center") return "text-center"
    return "text-left"
}

/** Row interactions, stable across renders so unchanged rows skip re-rendering. */
interface RowEvents<T> {
    click: (item: T) => void
    doubleClick: (item: T) => void
    toggle: (id: string, shiftKey: boolean) => void
    cellClick: (item: T, columnKey: string, event: React.MouseEvent) => void
    dismissSummary: () => void
}

interface DataTableRowProps<T extends { id: string }> {
    item: T
    columns: Column<T>[]
    isSelected: boolean
    isFocused: boolean
    /** The current column, given to the current row only. */
    currentColumnKey: string | null
    /** Selection totals shown at the current cell, given to the current row only. */
    summary: ReturnType<typeof summarize> | null
    /** Shift+click extends the selection, so it must not select text. */
    rangeSelect: boolean
    compact: boolean
    renderPrefix?: (item: T) => React.ReactNode
    rowClassName?: (item: T) => string | undefined
    events: RowEvents<T>
}

function DataTableRowImpl<T extends { id: string }>({
    item,
    columns,
    isSelected,
    isFocused,
    currentColumnKey,
    summary,
    rangeSelect,
    compact,
    renderPrefix,
    rowClassName,
    events,
}: DataTableRowProps<T>) {
    const rowH = compact ? "h-7" : "h-9"
    const cellPx = compact ? "px-2" : "px-4"
    return (
        <tr
            data-row-id={item.id}
            tabIndex={-1}
            data-focused={isFocused ? "true" : undefined}
            data-selected={isSelected ? "true" : undefined}
            aria-current={isFocused ? "true" : undefined}
            className={cn(
                // The sticky header and the bottom banner must not cover a row revealed by the keyboard.
                "cursor-pointer border-b border-border/60 transition-colors scroll-mt-10 scroll-mb-12",
                rowH,
                (isFocused || isSelected) && "shadow-[inset_2px_0_0_hsl(var(--ring))]",
                isFocused
                    ? "bg-accent ring-1 ring-inset ring-ring/55"
                    : isSelected
                        ? "bg-accent/85"
                        : "hover:bg-accent/65",
                rowClassName?.(item)
            )}
            onClick={() => events.click(item)}
            onDoubleClick={() => events.doubleClick(item)}
        >
            {/* Checkbox cell — same fixed width as header, centered */}
            <td className="w-[40px] min-w-[40px] max-w-[40px] px-0 py-0">
                <div className={cn("flex items-center justify-center", rowH)}>
                    <Checkbox
                        checked={isSelected}
                        tabIndex={-1}
                        aria-label="Выбрать строку"
                        onClick={(e: React.MouseEvent) => {
                            e.stopPropagation()
                            events.toggle(item.id, e.shiftKey)
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
                const isCurrentCell = currentColumnKey === col.key
                const cell = (
                    <td
                        key={col.key}
                        data-column-key={col.key}
                        data-current-cell={isCurrentCell ? "true" : undefined}
                        className={cn(
                            "py-0 scroll-mt-10 scroll-mb-12",
                            cellPx, rowH,
                            alignClass(col.align),
                            isCurrentCell && "relative z-[1] shadow-[inset_0_0_0_1px_hsl(var(--ring))]",
                            col.className
                        )}
                        onMouseDown={(event) => { if (event.shiftKey && rangeSelect) event.preventDefault() }}
                        onClick={(event) => events.cellClick(item, col.key, event)}
                    >
                        <div className={cn("flex items-center min-w-0 whitespace-nowrap overflow-hidden text-ellipsis", rowH,
                            col.align === "right" && "justify-end", col.align === "center" && "justify-center")}>
                            {col.render
                                ? col.render(item)
                                : String((item as JsonObject)[col.key] ?? "")}
                        </div>
                    </td>
                )
                return isCurrentCell && summary ? (
                    <SelectionSummaryPopover key={col.key} summary={summary} label={col.label} onDismiss={events.dismissSummary}>
                        {cell}
                    </SelectionSummaryPopover>
                ) : cell
            })}
        </tr>
    )
}

// memo() drops the type parameter; the cast restores it.
const DataTableRow = React.memo(DataTableRowImpl) as typeof DataTableRowImpl

/** The list page (marked data-list-page) whose toolbar hands focus back to the table. */
const LIST_PAGE_SELECTOR = "[data-list-page]"
/** Menus, dialogs and popovers: focus inside them still belongs to what opened them. */
export const OVERLAY_SELECTOR = '[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"], [data-radix-popper-content-wrapper]'
const OPEN_OVERLAY_SELECTOR = '[role="dialog"][data-state="open"], [role="alertdialog"][data-state="open"], [role="menu"][data-state="open"]'

/** Rows that fit the table's scroll viewport below its header: a PageUp/PageDown step. */
function visibleRowCount(table: HTMLTableElement): number {
    const viewport = table.closest<HTMLElement>("[data-radix-scroll-area-viewport]") ?? table.parentElement
    const rowHeight = table.tBodies[0]?.rows[0]?.offsetHeight || 36
    const header = table.tHead?.offsetHeight ?? 0
    return Math.max(1, Math.floor(((viewport?.clientHeight ?? 0) - header) / rowHeight) - 1)
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
    const shownSummary = summary && dismissedSummary !== summaryKey ? summary : null

    // Rows receive one stable set of handlers that call the latest props, so
    // moving the current row re-renders only the two rows whose state changed.
    const latestRef = useRef({ onRowClick, onRowDoubleClick, onToggleItem, handleCellClick, summaryKey })
    useEffect(() => {
        latestRef.current = { onRowClick, onRowDoubleClick, onToggleItem, handleCellClick, summaryKey }
    })
    const rowEvents = useMemo<RowEvents<T>>(() => ({
        click: (item) => latestRef.current.onRowClick?.(item),
        doubleClick: (item) => latestRef.current.onRowDoubleClick?.(item),
        toggle: (id, shiftKey) => latestRef.current.onToggleItem(id, shiftKey),
        cellClick: (item, columnKey, event) => latestRef.current.handleCellClick(item, columnKey, event),
        dismissSummary: () => setDismissedSummary(latestRef.current.summaryKey),
    }), [])

    // One context menu serves the whole body: the row under the pointer when
    // it opens becomes its subject (1C / Windows Explorer pattern: a row in
    // the selection targets the whole selection, any other row only itself).
    const [menuRowId, setMenuRowId] = useState<string | null>(null)
    const menuRow = menuRowId === null ? undefined : data.find((row) => row.id === menuRowId)
    const menuTargets = useMemo(() => {
        if (!menuRow) return []
        return selectedSet.has(menuRow.id) ? data.filter((row) => selectedSet.has(row.id)) : [menuRow]
    }, [menuRow, selectedSet, data])
    const rowIdAt = (target: EventTarget) =>
        target instanceof Element ? target.closest<HTMLElement>("tr[data-row-id]")?.dataset.rowId ?? null : null
    const noteMenuRow = (event: React.MouseEvent) => {
        const id = rowIdAt(event.target)
        // Prevented, the event opens no menu: there is no row to act on.
        if (id === null) event.preventDefault()
        else setMenuRowId(id)
    }
    // A touch long-press opens the menu without a contextmenu event.
    const noteTouchMenuRow = (event: React.PointerEvent) => {
        if (event.pointerType !== "mouse") setMenuRowId(rowIdAt(event.target))
    }

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

    // Keyboard navigation (1C style) while focus is in the table or nowhere:
    // ↑/↓, PageUp/PageDown, Home/End move the current row, and with Shift
    // select the rows from the anchor to it; ←/→ move the current column;
    // Enter opens the current row. Keys typed into an editor are left alone.
    const navigateRef = useRef<(event: KeyboardEvent) => void>(() => {})
    useEffect(() => {
        navigateRef.current = (event) => {
            if (event.defaultPrevented || event.altKey || event.metaKey || event.ctrlKey) return
            const table = tableRef.current
            if (!table || table.offsetParent === null) return
            const active = document.activeElement
            if (isEditable(active)) return
            const idle = !active || active === document.body
            if (!idle && !table.contains(active)) return

            const reveal = (id: string, columnKey: string | null) => {
                const row = table.querySelector<HTMLElement>(`tr[data-row-id="${CSS.escape(id)}"]`)
                if (!row) return
                const cell = columnKey ? row.querySelector<HTMLElement>(`td[data-column-key="${CSS.escape(columnKey)}"]`) : null
                ;(cell ?? row).scrollIntoView({ block: "nearest", inline: "nearest" })
                row.focus({ preventScroll: true })
            }
            const index = focusedId ? data.findIndex((row) => row.id === focusedId) : -1

            if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
                if (event.shiftKey || index === -1 || columns.length === 0) return
                const at = columns.findIndex((col) => col.key === focusedColumnKey)
                const step = event.key === "ArrowRight" ? 1 : -1
                const next = at === -1 ? 0 : Math.min(columns.length - 1, Math.max(0, at + step))
                event.preventDefault()
                onCellClick?.(data[index], columns[next].key)
                reveal(data[index].id, columns[next].key)
                return
            }
            if (event.key === "Enter") {
                // A focused control in a cell (a button, a link) handles Enter itself.
                if (event.shiftKey || index === -1 || !onRowDoubleClick || !(idle || active?.tagName === "TR")) return
                event.preventDefault()
                onRowDoubleClick(data[index])
                return
            }

            const target = rowMoveTarget(event.key, index, data.length, () => visibleRowCount(table))
            if (target === null) return
            event.preventDefault()
            const item = data[target]
            if (event.shiftKey && onSelectRows) {
                const anchor = anchorRef.current ?? focusedId ?? item.id
                onSelectRows(rowRange(data.map((row) => row.id), anchor, item.id), "replace")
                anchorRef.current = anchor
            } else {
                anchorRef.current = item.id
            }
            onRowClick?.(item)
            if (focusedColumnKey) onCellClick?.(item, focusedColumnKey)
            reveal(item.id, focusedColumnKey)
        }
    })
    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => navigateRef.current(event)
        document.addEventListener("keydown", onKeyDown)
        return () => document.removeEventListener("keydown", onKeyDown)
    }, [])

    // 1C style: when a menu or dialog of this list closes and hands focus
    // back to a toolbar button, focus returns to the current row, so the
    // keyboard keeps working in the table. Focus given to an editor (search,
    // a filter field) or to anything outside the list page stays where it is.
    useEffect(() => {
        let fromOverlay = false
        const noteOrigin = (event: Event) => {
            fromOverlay = event.target instanceof Element && event.target.closest(OVERLAY_SELECTOR) !== null
        }
        const onFocusIn = (event: FocusEvent) => {
            if (!fromOverlay) return
            const target = event.target
            const table = tableRef.current
            if (!(target instanceof HTMLElement) || !table || table.contains(target) || isEditable(target)) return
            if (target.closest(OVERLAY_SELECTOR)) return
            const page = table.closest(LIST_PAGE_SELECTOR)
            if (!page?.contains(target)) return
            fromOverlay = false
            requestAnimationFrame(() => {
                // Another overlay may have opened meanwhile (a menu item that opens a dialog).
                if (document.activeElement !== target || document.querySelector(OPEN_OVERLAY_SELECTOR)) return
                const row = table.querySelector<HTMLElement>('tr[data-focused="true"]')
                if (row) row.focus({ preventScroll: true })
                else target.blur()
            })
        }
        document.addEventListener("pointerdown", noteOrigin, true)
        document.addEventListener("keydown", noteOrigin, true)
        document.addEventListener("focusin", onFocusIn)
        return () => {
            document.removeEventListener("pointerdown", noteOrigin, true)
            document.removeEventListener("keydown", noteOrigin, true)
            document.removeEventListener("focusin", onFocusIn)
        }
    }, [])

    // Determine the checked state for the header checkbox
    const headerChecked = isAllSelected
        ? true
        : isIndeterminate
            ? ("indeterminate" as const)
            : false

    const rows = data.map((item) => {
        const isFocused = focusedId === item.id
        return (
            <DataTableRow
                key={item.id}
                item={item}
                columns={columns}
                isSelected={selectedSet.has(item.id)}
                isFocused={isFocused}
                currentColumnKey={isFocused ? focusedColumnKey : null}
                summary={isFocused ? shownSummary : null}
                rangeSelect={Boolean(onSelectRows)}
                compact={compact}
                renderPrefix={renderPrefix}
                rowClassName={rowClassName}
                events={rowEvents}
            />
        )
    })

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
                    <th scope="col" className={cn("w-[40px] min-w-[40px] max-w-[40px] px-0", thPy)}>
                        <div className="flex items-center justify-center">
                            <Checkbox
                                checked={headerChecked}
                                onCheckedChange={onToggleAll}
                                aria-label="Выбрать все строки"
                            />
                        </div>
                    </th>

                    {/* Optional prefix column (status icon, etc.) */}
                    {renderPrefix && (
                        <th scope="col" className={cn("w-[32px] min-w-[32px] max-w-[32px] px-1", thPy)}>
                            <span className="sr-only">Состояние</span>
                        </th>
                    )}

                    {/* Data columns with resize handles */}
                    {columns.map((col, colIndex) => {
                        const sortable = Boolean(col.sortable && onSort)
                        const sorted = sortable && sortColumn === col.key
                        const label = (
                            <>
                                <span className="truncate">{col.label}</span>
                                {sortable && (
                                    <SortIcon
                                        colKey={col.key}
                                        sortColumn={sortColumn}
                                        sortDirection={sortDirection}
                                    />
                                )}
                            </>
                        )
                        return (
                            <th
                                key={col.key}
                                scope="col"
                                aria-sort={sorted ? (sortDirection === "asc" ? "ascending" : "descending") : undefined}
                                data-column-key={col.key}
                                data-focused-column={focusedColumnKey === col.key ? "true" : undefined}
                                className={cn(
                                    "relative text-xs font-medium text-muted-foreground select-none",
                                    alignClass(col.align),
                                    sortable ? "p-0" : [cellPx, thPy],
                                    focusedColumnKey === col.key && "text-foreground",
                                    col.className
                                )}
                            >
                                {sortable ? (
                                    // The button fills the cell, so the whole header stays the click target.
                                    <button
                                        type="button"
                                        className={cn(
                                            "group flex w-full min-h-6 min-w-0 items-center font-medium transition-colors hover:text-foreground",
                                            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                                            cellPx, thPy,
                                            col.align === "right" && "justify-end",
                                            col.align === "center" && "justify-center"
                                        )}
                                        onClick={() => { if (!isResizing) onSort?.(col.key) }}
                                    >
                                        {label}
                                    </button>
                                ) : (
                                    <div className="truncate">{col.label}</div>
                                )}

                                {/* Resize handle */}
                                {onResizeStart && (
                                    <div
                                        aria-hidden="true"
                                        className="absolute right-0 top-0 h-full w-[5px] cursor-col-resize z-20 group/resize hover:bg-primary/30 active:bg-primary/50"
                                        onMouseDown={(e) => onResizeStart(colIndex, e)}
                                        onClick={(e) => e.stopPropagation()}
                                    >
                                        <div className="absolute right-0 top-1/4 h-1/2 w-[1px] bg-border group-hover/resize:bg-primary/60" />
                                    </div>
                                )}
                            </th>
                        )
                    })}
                </tr>
            </thead>
            {renderContextMenu ? (
                <ContextMenu onOpenChange={(open) => { if (!open) setMenuRowId(null) }}>
                    <ContextMenuTrigger asChild onContextMenu={noteMenuRow} onPointerDown={noteTouchMenuRow}>
                        <tbody className="bg-workspace-surface">{rows}</tbody>
                    </ContextMenuTrigger>
                    <ContextMenuContent className="w-56">
                        {menuRow && renderContextMenu(menuRow, menuTargets)}
                    </ContextMenuContent>
                </ContextMenu>
            ) : (
                <tbody className="bg-workspace-surface">{rows}</tbody>
            )}
        </table>
    )
}

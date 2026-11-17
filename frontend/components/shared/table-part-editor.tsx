"use client"

import React, { useEffect, useMemo, useRef, useState } from "react"
import { ArrowDown, ArrowUp, CopyPlus, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { ReferenceField } from "@/components/shared/reference-field"
import { OVERLAY_SELECTOR } from "@/components/shared/data-table"
import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { resolveRefEndpoint } from "@/lib/reference-endpoint"
import { amountUnitPairs, displayValue } from "@/lib/form-layout"
import { EMPTY_ROW_SELECTION, clickRow, keepExisting, rowMoveTarget, summaryRows, type RowSelection } from "@/lib/row-selection"
import { fieldSummaryExtractor, summarize } from "@/lib/selection-summary"
import { SelectionSummaryPopover, copyText } from "@/components/shared/selection-summary-popover"
import { useTablePartCellMode } from "@/hooks/useCompactMode"
import { cn } from "@/lib/utils"
import {
    LINE_IDENTITY_COLUMNS, addRow, copyRows, isEditableColumn, moveRows, referenceDisplayName, removeRows,
    restoreRow, rowKey, setCell,
} from "@/lib/table-part-rows"

export interface TablePartEditorProps {
    part: { name: string; label?: string; columns: MetadataField[] }
    rows: JsonObject[]
    /** Line fields the form's request accepts; null when the part is not writable. */
    writable: readonly string[] | null
    readOnly: boolean
    disabled: boolean
    hiddenColumns: ReadonlySet<string>
    /** On a tab the tab names the part, so the editor shows no title of its own. */
    bare?: boolean
    onChange: (rows: JsonObject[]) => void
    /** Renders a non-reference editor (the form's own field inputs). */
    renderInput: (column: MetadataField, value: JsonValue | undefined, onChange: (value: JsonValue) => void) => React.ReactNode
}

const NUMERIC = new Set(["integer", "number", "decimal", "money"])
/** Rows a PageUp/PageDown step moves in a table part (it grows with the page, not scrolls). */
const PAGE_ROWS = 10
const CURRENT_CELL = "shadow-[inset_0_0_0_1px_hsl(var(--ring))]"

/** "1 строка", "3 строки", "5 строк". */
function linesLabel(count: number): string {
    const tens = count % 100
    const ones = count % 10
    if (tens >= 11 && tens <= 14) return `${count} строк`
    if (ones === 1) return `${count} строка`
    if (ones >= 2 && ones <= 4) return `${count} строки`
    return `${count} строк`
}

/**
 * A command of the part's command bar: an icon (with a caption for the main
 * command) and a tooltip naming it with its key. It never takes focus, so the
 * keyboard stays in the grid or the cell being edited.
 */
function CommandButton({ label, shortcut, icon, caption, disabled, destructive, onClick }: {
    label: string
    shortcut?: string
    icon: React.ReactNode
    caption?: string
    disabled: boolean
    destructive?: boolean
    onClick: () => void
}) {
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button type="button" variant="ghost" size="sm" disabled={disabled} aria-label={label}
                    aria-keyshortcuts={shortcut}
                    className={cn("h-7 gap-1.5 px-2 text-xs font-normal", !caption && "w-7 px-0",
                        destructive && "hover:bg-destructive/10 hover:text-destructive")}
                    onMouseDown={(event) => event.preventDefault()} onClick={onClick}>
                    {icon}
                    {caption}
                </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
                {label}
                {shortcut ? <kbd className="ml-2 font-sans text-muted-foreground">{shortcut}</kbd> : null}
            </TooltipContent>
        </Tooltip>
    )
}

function cellText(row: JsonObject, column: MetadataField): string {
    if (column.type === "reference") return referenceDisplayName(row, column.name) ?? String(row[column.name] ?? "")
    if (column.type === "boolean") return row[column.name] ? "Да" : "Нет"
    const text = displayValue(column, row[column.name])
    return text === "—" ? "" : text
}

/**
 * A document table part as a grid: № from the position, one column per line
 * field. A writable part adds, removes and reorders lines; a cell is editable
 * when the line request accepts its field.
 *
 * With the user's "table parts like 1C" setting (cell mode) cells show their
 * values and a click only makes a cell current; a double click, Enter or F2
 * opens it for editing, Enter or Tab finish and Escape cancels. The keyboard
 * then works the grid: arrows, PageUp/PageDown, Home/End and Tab move, Shift
 * extends the selection, Ctrl/Cmd+A selects all lines, Ctrl/Cmd+C copies,
 * Delete (Cmd+Backspace) removes the selected lines, Insert adds one, F9
 * copies the selected lines and Ctrl/Cmd+Shift+↑/↓ moves the current one.
 */
export function TablePartEditor({ part, rows, writable, readOnly, disabled, hiddenColumns, bare = false, onChange, renderInput }: TablePartEditorProps) {
    const cellMode = useTablePartCellMode()
    const columns = part.columns.filter((column) => !hiddenColumns.has(column.name) && !LINE_IDENTITY_COLUMNS.has(column.name))
    const editable = !readOnly && writable !== null
    const title = part.label || part.name

    // 1C-style row selection: click, Shift+click range, Cmd/Ctrl+click toggle.
    // With the current row in a selection of several, a numeric current
    // column shows its totals beside the current cell.
    const keys = rows.map(rowKey)
    const [selectionState, setSelection] = useState<RowSelection>(EMPTY_ROW_SELECTION)
    const selection = keepExisting(selectionState, keys)
    const [currentColumn, setCurrentColumn] = useState<string | null>(null)
    const [dismissedSummary, setDismissedSummary] = useState("")
    const extractors = useMemo(() => {
        const units = amountUnitPairs(part.columns, true)
        return new Map(part.columns.map((column) => [column.name, fieldSummaryExtractor(column, column.name, units.get(column.name))]))
    }, [part.columns])
    const totalsRows = summaryRows(selection)
    const extractor = currentColumn ? extractors.get(currentColumn) : undefined
    const summary = totalsRows && extractor ? summarize(rows.filter((row, index) => totalsRows.includes(keys[index])).map(extractor)) : null
    const summaryKey = summary ? `${currentColumn}|${totalsRows?.join(",")}` : ""
    const selectedSet = new Set(selection.selected)

    // Cell mode: the cell open for editing and its row as it was, for Escape.
    // A line added by Insert is taken back by Escape while it is still open.
    const [editing, setEditing] = useState<{ key: string; column: string; original: JsonObject; added?: boolean } | null>(null)
    const editingCell = editing && keys.includes(editing.key) ? `${editing.key}|${editing.column}` : ""
    const gridRef = useRef<HTMLDivElement>(null)
    const revealNext = useRef(false)
    const focusGrid = () => gridRef.current?.focus({ preventScroll: true })

    // An opened cell takes focus with its value selected, ready to be typed over.
    useEffect(() => {
        if (!editingCell) return
        const cell = gridRef.current?.querySelector<HTMLElement>('td[data-editing="true"]')
        const control = cell?.querySelector<HTMLElement>("input, textarea, select, button, [tabindex]:not([tabindex='-1'])")
        control?.focus()
        if (control instanceof HTMLInputElement && control.type !== "checkbox") control.select()
        // A reference cell opens straight into its picker, as 1C opens the choice.
        if (control?.getAttribute("role") === "combobox" && control.getAttribute("aria-expanded") === "false") control.click()
    }, [editingCell])

    // A keyboard move scrolls the current cell into view (new lines included).
    const currentKey = selection.current
    useEffect(() => {
        if (!revealNext.current) return
        revealNext.current = false
        const grid = gridRef.current
        const target = grid?.querySelector<HTMLElement>('td[data-current-cell="true"]') ?? grid?.querySelector<HTMLElement>('tr[data-current="true"]')
        target?.scrollIntoView({ block: "nearest", inline: "nearest" })
    }, [currentKey, currentColumn, rows.length])

    const select = (state: RowSelection) => {
        setSelection(state)
        revealNext.current = true
    }
    const only = (key: string): RowSelection => ({ selected: [key], current: key, anchor: key })

    const onCellMouseDown = (event: React.MouseEvent) => {
        // Shift/Cmd/Ctrl+click selects rows instead of selecting text or focusing an input.
        if (event.shiftKey || event.metaKey || event.ctrlKey) event.preventDefault()
    }
    const onCellClick = (key: string, column: string | null, event: React.MouseEvent) => {
        const toggle = event.metaKey || event.ctrlKey
        if (event.shiftKey || toggle) {
            // A selection click must not also open a picker or flip a switch in the cell.
            event.preventDefault()
            event.stopPropagation()
        }
        setSelection(clickRow(selection, keys, key, { shift: event.shiftKey, toggle }))
        if (column) setCurrentColumn(column)
        if (cellMode) {
            if (editing && (editing.key !== key || editing.column !== column)) setEditing(null)
            focusGrid()
        }
    }

    // ── Cell mode: editing ──────────────────────────────────────────────
    const startEdit = (key: string, column: MetadataField) => {
        const index = keys.indexOf(key)
        if (index === -1 || disabled || !isEditableColumn(column, writable, readOnly)) return
        setCurrentColumn(column.name)
        // A flag has nothing to type: it flips in place.
        if (column.type === "boolean") {
            onChange(setCell(rows, index, column.name, !rows[index][column.name]))
            return
        }
        setEditing({ key, column: column.name, original: rows[index] })
    }
    const finishEdit = (cancel: boolean) => {
        if (!editing) return
        if (cancel && editing.added) {
            // As in 1C, Escape on a line just added takes the line back.
            const index = keys.indexOf(editing.key)
            onChange(removeRows(rows, new Set([editing.key])))
            const landing = keys[index - 1] ?? keys[index + 1]
            select(landing ? only(landing) : EMPTY_ROW_SELECTION)
        } else if (cancel) {
            onChange(restoreRow(rows, editing.key, editing.original))
        }
        setEditing(null)
        focusGrid()
    }

    // ── Cell mode: lines ────────────────────────────────────────────────
    /** The lines a command acts on: the selection holding the current line, else the current line. */
    const targetKeys = (): Set<string> => {
        if (selection.current && selection.selected.includes(selection.current)) return new Set(selection.selected)
        return new Set(selection.current ? [selection.current] : [])
    }
    const insertLine = () => {
        const next = addRow(rows)
        const key = rowKey(next[next.length - 1], next.length - 1)
        onChange(next)
        select(only(key))
        const first = columns.find((column) => isEditableColumn(column, writable, readOnly))
        if (first) {
            setCurrentColumn(first.name)
            if (cellMode && first.type !== "boolean") setEditing({ key, column: first.name, original: next[next.length - 1], added: true })
        }
    }
    const deleteLines = () => {
        const targets = targetKeys()
        if (targets.size === 0) return
        const first = keys.findIndex((key) => targets.has(key))
        const next = removeRows(rows, targets)
        onChange(next)
        const landing = next.length ? rowKey(next[Math.min(first, next.length - 1)], Math.min(first, next.length - 1)) : null
        select(landing ? only(landing) : EMPTY_ROW_SELECTION)
    }
    const duplicateLines = () => {
        if (!writable) return
        const { rows: next, copied } = copyRows(rows, targetKeys(), writable)
        if (copied.length === 0) return
        onChange(next)
        select({ selected: copied, current: copied[0], anchor: copied[0] })
    }
    // The selected lines move as a block; the current line alone when it is outside the selection.
    const movedLines = (step: -1 | 1) => moveRows(rows, targetKeys(), step)
    const moveLines = (step: -1 | 1) => {
        const next = movedLines(step)
        if (!next) return
        onChange(next)
        revealNext.current = true
    }
    const copyToClipboard = () => {
        const targets = targetKeys()
        let text = ""
        if (targets.size > 1) {
            text = rows.filter((_, index) => targets.has(keys[index]))
                .map((row) => columns.map((column) => cellText(row, column)).join("\t"))
                .join("\n")
        } else {
            const index = selection.current ? keys.indexOf(selection.current) : -1
            const column = columns.find((col) => col.name === currentColumn)
            if (index !== -1 && column) text = cellText(rows[index], column)
        }
        void copyText(text).then((copied) => {
            if (!copied) toast.error("Не удалось скопировать")
            else if (targets.size > 1) toast.success(`Скопировано строк: ${targets.size}`)
        })
    }
    const moveColumn = (step: -1 | 1): boolean => {
        const at = columns.findIndex((column) => column.name === currentColumn)
        const next = at === -1 ? 0 : at + step
        if (next < 0 || next >= columns.length) return false
        setCurrentColumn(columns[next].name)
        if (!selection.current && keys[0]) select(only(keys[0]))
        revealNext.current = true
        return true
    }

    const onGridKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
        const handled = () => {
            // Handled keys end here: the form's and the app's shortcuts do not see them.
            event.preventDefault()
            event.stopPropagation()
        }
        if (editing) {
            // A picker or menu inside the cell handles its own keys first.
            if (event.defaultPrevented) return
            if (event.key === "Escape") { handled(); finishEdit(true); return }
            if (event.key === "Enter" && !event.shiftKey && !(event.target instanceof HTMLTextAreaElement)) { handled(); finishEdit(false); return }
            if (event.key === "Tab") {
                setEditing(null)
                if (moveColumn(event.shiftKey ? -1 : 1)) { handled(); focusGrid() }
            }
            return
        }
        if (event.target !== event.currentTarget || event.altKey) return
        const index = selection.current ? keys.indexOf(selection.current) : -1
        const column = columns.find((col) => col.name === currentColumn)
        if (event.metaKey || event.ctrlKey) {
            if (event.code === "KeyA" && !event.shiftKey && keys.length) {
                handled()
                setSelection({ selected: keys, current: selection.current ?? keys[0], anchor: selection.anchor ?? keys[0] })
                if (!currentColumn && columns[0]) setCurrentColumn(columns[0].name)
            } else if (event.code === "KeyC" && !event.shiftKey) {
                handled()
                copyToClipboard()
            } else if (event.shiftKey && (event.key === "ArrowUp" || event.key === "ArrowDown") && editable && !disabled) {
                handled()
                moveLines(event.key === "ArrowUp" ? -1 : 1)
            } else if (event.key === "Backspace" && editable && !disabled) {
                handled()
                deleteLines()
            }
            return
        }
        const target = rowMoveTarget(event.key, index, rows.length, PAGE_ROWS)
        if (target !== null) {
            handled()
            select(clickRow(selection, keys, keys[target], { shift: event.shiftKey, toggle: false }))
            if (!currentColumn && columns[0]) setCurrentColumn(columns[0].name)
            return
        }
        switch (event.key) {
            case "ArrowLeft":
            case "ArrowRight":
                if (!event.shiftKey && moveColumn(event.key === "ArrowRight" ? 1 : -1)) handled()
                return
            case "Tab":
                // At the edge Tab leaves the grid, so the keyboard is never trapped in it.
                if (moveColumn(event.shiftKey ? -1 : 1)) handled()
                return
            case "Enter":
            case "F2":
            case " ":
                if (index === -1 || !column || !isEditableColumn(column, writable, readOnly) || disabled) return
                if (event.key === " " && column.type !== "boolean") return
                handled()
                startEdit(keys[index], column)
                return
            case "Delete":
                if (editable && !disabled) { handled(); deleteLines() }
                return
            case "Insert":
                if (editable && !disabled) { handled(); insertLine() }
                return
            case "F9":
                if (editable && !disabled) { handled(); duplicateLines() }
                return
            case "Escape":
                if (selection.current && selection.selected.length > 1) { handled(); setSelection(only(selection.current)) }
                return
        }
    }

    // Command bar state: what the commands would act on, and where the user is.
    const targetCount = targetKeys().size
    const hasTarget = targetCount > 0
    const hasMany = targetCount > 1
    const currentIndex = selection.current ? keys.indexOf(selection.current) : -1
    const statusText = hasMany
        ? `Выделено ${targetCount} из ${rows.length}`
        : currentIndex !== -1 ? `Строка ${currentIndex + 1} из ${rows.length}` : linesLabel(rows.length)

    const onGridBlur = (event: React.FocusEvent<HTMLDivElement>) => {
        // Focus leaving for outside the grid closes the cell; a picker's popover is still the cell's.
        if (!editing) return
        const next = event.relatedTarget
        if (next instanceof Element && (event.currentTarget.contains(next) || next.closest(OVERLAY_SELECTOR))) return
        setEditing(null)
    }

    return (
        <section className={bare ? "space-y-2" : "mt-8 space-y-2"} aria-label={title} data-table-part={part.name}>
            {!bare && (
                <div className="flex items-baseline gap-2">
                    <h3 className="text-sm font-semibold">{title}</h3>
                    <span className="text-xs tabular-nums text-muted-foreground">{rows.length || ""}</span>
                </div>
            )}
            <div
                ref={gridRef}
                className={cn("overflow-hidden rounded-md border bg-card text-sm shadow-control", cellMode && "outline-none focus-visible:ring-1 focus-visible:ring-ring")}
                {...(cellMode ? { tabIndex: 0, onKeyDown: onGridKeyDown, onBlur: onGridBlur, "data-cell-mode": "true" } : {})}
            >
                {editable && (
                    // 1C command bar: line commands sit between the title and the grid
                    // and act on the selected lines (else the current one).
                    <TooltipProvider delayDuration={400}>
                        <div role="toolbar" aria-label={`Команды: ${title}`}
                            className="flex items-center gap-0.5 border-b bg-muted/30 px-1.5 py-1">
                            <CommandButton label="Добавить строку" shortcut={cellMode ? "Ins" : undefined} caption="Добавить"
                                icon={<Plus className="h-3.5 w-3.5" />} disabled={disabled} onClick={insertLine} />
                            <CommandButton label={hasMany ? "Скопировать строки" : "Скопировать строку"} shortcut={cellMode ? "F9" : undefined}
                                icon={<CopyPlus className="h-3.5 w-3.5" />} disabled={disabled || !hasTarget} onClick={duplicateLines} />
                            <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                            <CommandButton label="Переместить вверх" shortcut={cellMode ? "Ctrl+Shift+↑" : undefined}
                                icon={<ArrowUp className="h-3.5 w-3.5" />} disabled={disabled || !movedLines(-1)} onClick={() => moveLines(-1)} />
                            <CommandButton label="Переместить вниз" shortcut={cellMode ? "Ctrl+Shift+↓" : undefined}
                                icon={<ArrowDown className="h-3.5 w-3.5" />} disabled={disabled || !movedLines(1)} onClick={() => moveLines(1)} />
                            <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                            <CommandButton label={hasMany ? "Удалить строки" : "Удалить строку"} shortcut={cellMode ? "Del" : undefined} destructive
                                icon={<Trash2 className="h-3.5 w-3.5" />} disabled={disabled || !hasTarget} onClick={deleteLines} />
                            <span className="ml-auto px-1.5 text-xs tabular-nums text-muted-foreground" aria-live="polite">{statusText}</span>
                        </div>
                    </TooltipProvider>
                )}
                <div className="overflow-x-auto">
                <table className="w-full border-collapse">
                    <thead className="bg-muted/50">
                        <tr className="border-b">
                            <th scope="col" className="w-10 px-2 py-2 text-left font-medium">№</th>
                            {columns.map((column) => (
                                <th key={column.name} scope="col" className={cn("min-w-32 px-2 py-2 text-left font-medium", cellMode && NUMERIC.has(column.type) && "text-right")}>
                                    {column.label || column.name}{column.required && editable ? <span className="text-destructive"> *</span> : null}
                                </th>
                            ))}
                        </tr>
                    </thead>
                    <tbody>
                        {rows.length === 0 && (
                            <tr>
                                <td colSpan={columns.length + 1} className="px-3 py-4 text-center text-muted-foreground">
                                    {editable ? "Нет строк. Добавьте первую командой «Добавить»" : "Нет строк"}
                                </td>
                            </tr>
                        )}
                        {rows.map((row, index) => {
                            const lineNo = index + 1
                            const key = keys[index]
                            return (
                                <tr key={key} data-selected={selectedSet.has(key) ? "true" : undefined} data-current={selection.current === key ? "true" : undefined}
                                    className={cn("border-b align-middle last:border-0",
                                        selection.current === key ? "bg-accent" : selectedSet.has(key) && "bg-accent/50")}>
                                    <td className="cursor-pointer select-none px-2 py-1 tabular-nums text-muted-foreground"
                                        onMouseDown={onCellMouseDown} onClick={(event) => onCellClick(key, null, event)}>{lineNo}</td>
                                    {columns.map((column) => {
                                        const cellEditable = isEditableColumn(column, writable, readOnly)
                                        const isCurrentCell = selection.current === key && currentColumn === column.name
                                        const isEditing = cellMode && editingCell === `${key}|${column.name}`
                                        const withTotals = (cell: React.ReactElement) => isCurrentCell && summary && dismissedSummary !== summaryKey ? (
                                            <SelectionSummaryPopover key={column.name} summary={summary} label={column.label || column.name} onDismiss={() => setDismissedSummary(summaryKey)}>
                                                {cell}
                                            </SelectionSummaryPopover>
                                        ) : cell

                                        if (cellMode && !isEditing) {
                                            // Cell mode: the value as text; a double click opens it.
                                            return withTotals(
                                                <td key={column.name} data-current-cell={isCurrentCell ? "true" : undefined}
                                                    className={cn("h-9 max-w-48 cursor-default select-none truncate px-2 py-1",
                                                        NUMERIC.has(column.type) && "text-right tabular-nums",
                                                        !cellEditable && "text-muted-foreground",
                                                        isCurrentCell && CURRENT_CELL)}
                                                    onMouseDown={onCellMouseDown}
                                                    onClick={(event) => onCellClick(key, column.name, event)}
                                                    onDoubleClick={(event) => { if (!event.shiftKey && !event.metaKey && !event.ctrlKey) startEdit(key, column) }}>
                                                    {column.type === "boolean"
                                                        ? <Checkbox checked={!!row[column.name]} tabIndex={-1} aria-hidden className="pointer-events-none" />
                                                        : cellText(row, column)}
                                                </td>,
                                            )
                                        }

                                        const selectHandlers = cellMode ? {} : {
                                            onMouseDownCapture: onCellMouseDown,
                                            onClickCapture: (event: React.MouseEvent) => onCellClick(key, column.name, event),
                                            onFocusCapture: () => { setCurrentColumn(column.name); if (selection.current !== key) setSelection(only(key)) },
                                        }
                                        if (!cellEditable) {
                                            return withTotals(<td key={column.name} data-current-cell={isCurrentCell ? "true" : undefined} className={cn("max-w-48 truncate px-2 py-1", isCurrentCell && CURRENT_CELL)} {...selectHandlers}>{cellText(row, column)}</td>)
                                        }
                                        const endpoint = column.type === "reference" && column.referenceType ? resolveRefEndpoint(column.referenceType) : null
                                        return (
                                            withTotals(<td key={column.name} data-current-cell={isCurrentCell ? "true" : undefined} data-editing={isEditing ? "true" : undefined}
                                                className={cn("px-1 py-1", isCurrentCell && CURRENT_CELL)} data-field={`${part.name}.${index}.${column.name}`} {...selectHandlers}>
                                                {endpoint ? (
                                                    <ReferenceField
                                                        value={typeof row[column.name] === "string" ? String(row[column.name]) : ""}
                                                        displayName={referenceDisplayName(row, column.name)}
                                                        onChange={(value, display) => {
                                                            onChange(setCell(rows, index, column.name, value, display))
                                                            if (isEditing) {
                                                                // Picking a value finishes the cell; focus returns to the
                                                                // grid once the picker has handed it back.
                                                                setEditing(null)
                                                                requestAnimationFrame(() => requestAnimationFrame(focusGrid))
                                                            }
                                                        }}
                                                        apiEndpoint={endpoint}
                                                        placeholder="Выберите…"
                                                        compact
                                                        disabled={disabled}
                                                    />
                                                ) : (
                                                    renderInput(column, row[column.name], (value) => onChange(setCell(rows, index, column.name, value)))
                                                )}
                                            </td>)
                                        )
                                    })}
                                </tr>
                            )
                        })}
                    </tbody>
                </table>
                </div>
            </div>
        </section>
    )
}

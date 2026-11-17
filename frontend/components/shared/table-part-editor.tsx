"use client"

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { ArrowDown, ArrowDownToLine, ArrowUp, ClipboardCopy, CopyPlus, PackagePlus, Plus, Trash2, Undo2 } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuShortcut, ContextMenuTrigger } from "@/components/ui/context-menu"
import { ReferenceField } from "@/components/shared/reference-field"
import { OVERLAY_SELECTOR } from "@/components/shared/data-table"
import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { resolveRefEndpoint } from "@/lib/reference-endpoint"
import { amountUnitPairs, displayValue } from "@/lib/form-layout"
import { EMPTY_ROW_SELECTION, clickRow, keepExisting, rowMoveTarget, summaryRows, type RowSelection } from "@/lib/row-selection"
import { fieldSummaryExtractor, isSummableField, summarize, summaryCopyText, summaryGroupText, type SelectionSummary } from "@/lib/selection-summary"
import { SelectionSummaryPopover, copyText } from "@/components/shared/selection-summary-popover"
import { useCompactMode, useTablePartCellMode } from "@/hooks/useCompactMode"
import { TablePartPickDialog } from "@/components/shared/table-part-pick-dialog"
import { documentQuantities, pickTarget, transferPicks, type CartLine } from "@/lib/pick-lines"
import { useColumnResize } from "@/hooks/useColumnResize"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { cn } from "@/lib/utils"
import {
    LINE_IDENTITY_COLUMNS, ROW_KEY, addRow, copyRows, fillColumn, isEditableColumn, moveRows, newRowKey, referenceDisplayName,
    removeRows, restoreRow, rowKey, setCell,
} from "@/lib/table-part-rows"
import { convertCellText, evaluateNumberInput, parseClipboardGrid, parseNumberText, pasteGrid } from "@/lib/grid-paste"
import { apiFetch } from "@/lib/api"
import { withReferenceListParams } from "@/lib/reference-utils"
import type { CursorListResponse } from "@/types/common"

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
    /** Key the user's column widths are kept under (entity and part); none keeps them for the page only. */
    widthsKey?: string
    /** A cell to take the user to (a required cell left empty); a new seq repeats the request. */
    focusRequest?: { line: number; column: string; seq: number } | null
}

const NUMERIC = new Set(["integer", "number", "decimal", "money"])
/** Rows a PageUp/PageDown step moves in a table part (it grows with the page, not scrolls). */
const PAGE_ROWS = 10
const CURRENT_CELL = "shadow-[inset_0_0_0_1px_hsl(var(--ring))]"
const ERROR_CELL = "shadow-[inset_0_0_0_1px_hsl(var(--destructive))] bg-destructive/5"
/** Width of the № column, px. */
const NUMBER_COLUMN = 44
/** Auto-fit bounds of a column, px. */
const FIT_MIN = 64
const FIT_MAX = 640
/** Air added to the widest content of a fitted column, px. */
const FIT_AIR = 6

/**
 * Line backgrounds are opaque (mixed with the card colour) so the frozen
 * № and item cells, which inherit them, hide what scrolls beneath.
 */
const ROW_BG = "bg-card hover:bg-[color:color-mix(in_srgb,hsl(var(--muted))_45%,hsl(var(--card)))]"
const SELECTED_BG = "bg-[color:color-mix(in_srgb,hsl(var(--accent))_55%,hsl(var(--card)))]"
const CURRENT_BG = "bg-accent"

/**
 * In-cell editors without a frame until hovered or focused (as in Google
 * Sheets and Airtable): the grid reads as data, not as a stack of inputs.
 */
const QUIET_EDITORS = cn(
    "[&_input]:border-transparent [&_input]:bg-transparent [&_input]:shadow-none hover:[&_input]:border-input",
    "[&_input:focus-visible]:border-ring [&_input:focus-visible]:bg-field",
    "[&_[role=combobox]]:border-transparent [&_[role=combobox]]:bg-transparent [&_[role=combobox]]:shadow-none hover:[&_[role=combobox]]:border-input",
    // One type size across the line: a picker reads like the text and numbers beside it.
    "[&_[role=combobox]]:text-sm",
    "[&_[role=combobox]_svg]:opacity-0 hover:[&_[role=combobox]_svg]:opacity-100 focus-within:[&_[role=combobox]_svg]:opacity-100",
)

/** A column's starting width by what it holds: references and texts wide, numbers and flags narrow. */
function defaultColumnWidth(column: MetadataField): number {
    if (column.type === "reference" || column.type === "typed_ref") return 240
    if (NUMERIC.has(column.type)) return 120
    if (column.type === "boolean") return 84
    if (column.type === "date") return 130
    if (column.type === "datetime") return 170
    if (column.enumValues?.length) return 160
    return 200
}

function isEmptyValue(value: JsonValue | undefined): boolean {
    return value === undefined || value === null || value === ""
}

/** A footer total: the sum per unit, a click copies it for a calculator. */
function FooterTotal({ summary }: { summary: SelectionSummary }) {
    const groups = summary.groups
    const copy = async () => {
        const text = groups.map((group) => summaryCopyText(group.sum)).join("\t")
        if (await copyText(text)) toast.success(`Скопировано: ${text}`)
        else toast.error("Не удалось скопировать")
    }
    return (
        <button type="button" tabIndex={-1} title="Скопировать итог" onMouseDown={(event) => event.preventDefault()} onClick={copy}
            className="w-full truncate rounded px-1 text-right tabular-nums hover:bg-accent">
            {groups.map(summaryGroupText).join(" · ")}
        </button>
    )
}

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

/** Number columns get the grid's own editor (calculator, "+5"); money keeps the form's minor-units input. */
const CALC_TYPES = new Set(["integer", "number", "decimal"])
/** Most steps the line history keeps for Ctrl+Z. */
const HISTORY_LIMIT = 50

/** Sets a React-controlled input's value as typing would, so its onChange runs. */
function typeInto(input: HTMLInputElement, text: string) {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(input, text)
    input.dispatchEvent(new Event("input", { bubbles: true }))
}

/**
 * A number cell's editor (1C and Excel style): takes "1 234,5", an
 * expression ("500*4") or a change of the value it had ("+5", "*2", "/4"),
 * worked out on Enter, Tab or leaving the cell. Plain numbers apply as they
 * are typed, so the footer follows.
 */
function NumberCellInput({ value, integer, disabled, onValue }: {
    value: JsonValue | undefined
    integer: boolean
    disabled: boolean
    onValue: (value: number) => void
}) {
    const [draft, setDraft] = useState<string | null>(null)
    const [start, setStart] = useState<number | null>(null)
    const shown = value === undefined || value === null || value === "" ? "" : String(value)
    const commit = () => {
        if (draft === null) return
        const result = evaluateNumberInput(draft, start, integer)
        setDraft(null)
        if (result !== null) onValue(result)
        else if (draft.trim()) toast.error(`Не число: «${draft.trim()}»`)
    }
    return (
        <Input type="text" inputMode="decimal" autoComplete="off" value={draft ?? shown} disabled={disabled}
            aria-invalid={draft !== null && draft.trim() !== "" && parseNumberText(draft) === null && evaluateNumberInput(draft, start, integer) === null}
            onChange={(event) => {
                const text = event.target.value
                if (draft === null) setStart(parseNumberText(shown))
                setDraft(text)
                const plain = parseNumberText(text)
                if (plain !== null && (!integer || Number.isInteger(plain))) onValue(plain)
            }}
            onBlur={commit}
            onKeyDown={(event) => {
                // Worked out here; in cell mode the grid then closes the cell. (The
                // form has no <form> element, so Enter submits nothing.)
                if (event.key === "Enter" || event.key === "Tab") {
                    commit()
                } else if (event.key === "Escape") {
                    setDraft(null)
                }
            }} />
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
export function TablePartEditor({ part, rows, writable, readOnly, disabled, hiddenColumns, bare = false, onChange, renderInput, widthsKey, focusRequest }: TablePartEditorProps) {
    const cellMode = useTablePartCellMode()
    const compact = useCompactMode()
    // 32 px lines (28 in compact mode): editors 28/24 px high in 2 px of padding.
    const rowHeight = compact ? "h-7" : "h-8"
    const editorHeight = compact ? "[&_input]:h-6" : "[&_input]:h-7"
    const columns = useMemo(
        () => part.columns.filter((column) => !hiddenColumns.has(column.name) && !LINE_IDENTITY_COLUMNS.has(column.name)),
        [part.columns, hiddenColumns],
    )
    const editable = !readOnly && writable !== null
    const title = part.label || part.name

    // 1C-style row selection: click, Shift+click range, Cmd/Ctrl+click toggle.
    // With the current row in a selection of several, a numeric current
    // column shows its totals beside the current cell.
    const keys = rows.map(rowKey)
    // The entry line (Excel, Airtable): an empty line after the last one;
    // typing into it makes it a line and a new entry line appears below. It
    // keeps its key when it becomes a line, so the cell being typed in stays.
    const [ghostKey, setGhostKey] = useState(newRowKey)
    const showGhost = editable
    const navKeys = showGhost ? [...keys, ghostKey] : keys
    const [selectionState, setSelection] = useState<RowSelection>(EMPTY_ROW_SELECTION)
    const selection = keepExisting(selectionState, navKeys)
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

    // Footer (1C "Подвал"): totals of the columns whose values add up, over all lines.
    const footer = columns.map((column) => {
        const extract = isSummableField(column) ? extractors.get(column.name) : undefined
        return extract && rows.length ? summarize(rows.map(extract)) : null
    })
    const hasFooter = footer.some(Boolean)

    // Column widths: by type at first, then as the user drags them, kept per entity and part.
    const storedWidths = useUserPrefsStore((state) => (widthsKey ? state.getListColumnWidths(widthsKey) : undefined))
    const saveWidths = useUserPrefsStore((state) => state.setListColumnWidths)
    const resizeDefs = useMemo(() => columns.map((column) => ({ key: column.name, width: defaultColumnWidth(column), minWidth: 64 })), [columns])
    const onWidthsChange = useCallback((widths: Record<string, number>) => { if (widthsKey) saveWidths(widthsKey, widths) }, [widthsKey, saveWidths])
    const { colWidths, onResizeStart, isResizing } = useColumnResize({ columns: resizeDefs, storedWidths, onWidthsChange })
    const tableWidth = NUMBER_COLUMN + colWidths.reduce((total, width) => total + width, 0)
    // № and the first column (the item, as a rule) stay put when the grid scrolls sideways.
    const frozenWidth = NUMBER_COLUMN + (columns.length > 1 ? colWidths[0] ?? 0 : 0)
    const scrollRef = useRef<HTMLDivElement>(null)
    /**
     * Double click on a column edge fits the column to its widest content, as
     * in Excel. Each cell's text is measured in its own font, and the room
     * around the text in that cell (padding, a picker's icons) is added.
     */
    const fitColumn = (index: number) => {
        const table = scrollRef.current?.querySelector("table")
        const context = document.createElement("canvas").getContext("2d")
        if (!table || !context || !widthsKey) return
        const padding = (element: HTMLElement) => {
            const style = getComputedStyle(element)
            return parseFloat(style.paddingLeft) + parseFloat(style.paddingRight)
        }
        let widest = 0
        table.querySelectorAll<HTMLElement>(`tr > :nth-child(${index + 2})`).forEach((cell) => {
            const input = cell.querySelector<HTMLInputElement>("input:not([type=checkbox])")
            // The text holder: an input, a picker's label (inside its button, past its icons), a footer total, else the cell.
            const holder = input ?? cell.querySelector<HTMLElement>("[role=combobox] > span:first-child") ?? cell.querySelector<HTMLElement>("button") ?? cell
            const text = input ? input.value : holder.innerText.trim()
            context.font = getComputedStyle(holder).font
            const around = holder === cell ? padding(cell) : cell.clientWidth - holder.clientWidth + padding(holder)
            widest = Math.max(widest, context.measureText(text).width + around)
        })
        const width = Math.min(FIT_MAX, Math.max(FIT_MIN, Math.ceil(widest) + FIT_AIR))
        saveWidths(widthsKey, Object.fromEntries(columns.map((column, i) => [column.name, i === index ? width : colWidths[i]])))
    }

    // The cell a failed save pointed at stays marked until it is filled.
    const [errorCell, setErrorCell] = useState("")

    // Cell mode: the cell open for editing and its row as it was, for Escape.
    // A line added by Insert is taken back by Escape while it is still open.
    const [editing, setEditing] = useState<{ key: string; column: string; original: JsonObject; added?: boolean } | null>(null)
    const editingCell = editing && navKeys.includes(editing.key) ? `${editing.key}|${editing.column}` : ""
    // A key typed on a cell (type-to-edit) goes into its editor once it opens.
    const seedRef = useRef<string | null>(null)

    // ── Line history (Ctrl+Z, "Вернуть") ────────────────────────────────
    // Each change keeps the lines it replaced; changes to one cell in a row
    // count as one step. Saving starts the history over.
    type HistoryStep = { before: JsonObject[]; label: string; cell?: string }
    const [history, setHistory] = useState<HistoryStep[]>([])
    const historyRef = useRef<HistoryStep[]>([])
    useEffect(() => { historyRef.current = history })
    const [wasDisabled, setWasDisabled] = useState(disabled)
    if (disabled !== wasDisabled) {
        setWasDisabled(disabled)
        if (disabled) setHistory([])
    }
    const change = (next: JsonObject[], label: string, cell?: string) => {
        const top = history[history.length - 1]
        if (!(cell && top?.cell === cell)) setHistory([...history, { before: rows, label, cell }].slice(-HISTORY_LIMIT))
        onChange(next)
    }
    const undo = () => {
        const steps = historyRef.current
        const step = steps[steps.length - 1]
        if (!step) return
        setHistory(steps.slice(0, -1))
        setEditing(null)
        onChange(step.before)
        toast(`Отменено: ${step.label}`)
    }
    const undoAction = { label: "Вернуть", onClick: () => undo() }
    /** Writes a cell; on the entry line it first makes that line a real one. */
    const updateCell = (index: number, column: string, value: JsonValue, display?: string) => {
        if (index < rows.length) {
            change(setCell(rows, index, column, value, display), "изменение ячейки", `${keys[index]}|${column}`)
            return
        }
        change(setCell([...rows, { [ROW_KEY]: ghostKey }], rows.length, column, value, display), "новая строка", `${ghostKey}|${column}`)
        setGhostKey(newRowKey())
    }
    const gridRef = useRef<HTMLDivElement>(null)
    const revealNext = useRef(false)
    const focusGrid = () => gridRef.current?.focus({ preventScroll: true })

    // An opened cell takes focus with its value selected, ready to be typed over.
    useEffect(() => {
        if (!editingCell) return
        const cell = gridRef.current?.querySelector<HTMLElement>('td[data-editing="true"]')
        const control = cell?.querySelector<HTMLElement>("input, textarea, select, button, [tabindex]:not([tabindex='-1'])")
        control?.focus()
        const seed = seedRef.current
        seedRef.current = null
        if (control instanceof HTMLInputElement && control.type !== "checkbox") {
            // Typed over: the key that opened the cell replaces its value.
            if (seed) typeInto(control, seed)
            else control.select()
        }
        // A reference cell opens straight into its picker, as 1C opens the
        // choice, with the typed key already in its search.
        if (control?.getAttribute("role") === "combobox" && control.getAttribute("aria-expanded") === "false") {
            control.click()
            if (seed) requestAnimationFrame(() => {
                const search = document.activeElement
                if (search instanceof HTMLInputElement) typeInto(search, seed)
            })
        }
    }, [editingCell])

    // Scrolls to the cell the form sent the user to and, with inputs in the
    // cells, focuses its input (cell mode opens the cell instead, see below).
    const focusSeq = focusRequest?.seq
    const focusField = focusRequest ? `${part.name}.${focusRequest.line}.${focusRequest.column}` : ""
    useEffect(() => {
        if (focusSeq === undefined || !focusField) return
        const frame = requestAnimationFrame(() => {
            const cell = gridRef.current?.querySelector<HTMLElement>(`td[data-field="${CSS.escape(focusField)}"]`)
            cell?.scrollIntoView({ block: "center", inline: "nearest" })
            if (!cellMode) cell?.querySelector<HTMLElement>("input, textarea, select, button")?.focus()
        })
        return () => cancelAnimationFrame(frame)
    }, [focusSeq, focusField, cellMode])

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

    // A cell the form sends the user to (a required cell left empty on save)
    // becomes current and marked, and in cell mode opens; each request once.
    const [handledFocusSeq, setHandledFocusSeq] = useState<number | null>(null)
    if (focusRequest && focusRequest.seq !== handledFocusSeq) {
        setHandledFocusSeq(focusRequest.seq)
        const key = keys[focusRequest.line]
        const column = columns.find((col) => col.name === focusRequest.column)
        if (key && column) {
            setSelection(only(key))
            setCurrentColumn(column.name)
            setErrorCell(`${key}|${column.name}`)
            if (cellMode && isEditableColumn(column, writable, readOnly) && column.type !== "boolean") {
                setEditing({ key, column: column.name, original: rows[focusRequest.line] })
            }
        }
    }

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
        setSelection(clickRow(selection, navKeys, key, { shift: event.shiftKey, toggle }))
        if (column) setCurrentColumn(column)
        if (cellMode) {
            if (editing && (editing.key !== key || editing.column !== column)) setEditing(null)
            focusGrid()
        }
    }

    // ── Cell mode: editing ──────────────────────────────────────────────
    const startEdit = (key: string, column: MetadataField, seed?: string) => {
        const index = navKeys.indexOf(key)
        if (index === -1 || disabled || !isEditableColumn(column, writable, readOnly)) return
        const ghost = key === ghostKey
        const row = ghost ? { [ROW_KEY]: ghostKey } : rows[index]
        setCurrentColumn(column.name)
        // A flag has nothing to type: it flips in place.
        if (column.type === "boolean") {
            updateCell(index, column.name, !row[column.name])
            return
        }
        seedRef.current = seed ?? null
        // A cell opened on the entry line is a line being added: Escape takes it back.
        setEditing({ key, column: column.name, original: row, added: ghost })
    }
    const finishEdit = (cancel: boolean) => {
        if (!editing) return
        if (cancel && editing.added) {
            // As in 1C, Escape on a line just added takes the line back.
            const index = keys.indexOf(editing.key)
            if (index !== -1) onChange(removeRows(rows, new Set([editing.key])))
            const landing = keys[index === -1 ? keys.length - 1 : index - 1] ?? keys[index + 1]
            select(landing ? only(landing) : EMPTY_ROW_SELECTION)
        } else if (cancel) {
            onChange(restoreRow(rows, editing.key, editing.original))
        }
        // A cancelled edit leaves nothing to undo.
        const top = history[history.length - 1]
        if (cancel && top?.cell === `${editing.key}|${editing.column}`) setHistory(history.slice(0, -1))
        setEditing(null)
        focusGrid()
    }

    // ── Cell mode: lines ────────────────────────────────────────────────
    /** The lines a command acts on: the selection holding the current line, else the current line. */
    const targetKeys = (): Set<string> => {
        const real = (list: readonly string[]) => new Set(list.filter((key) => key !== ghostKey))
        if (selection.current && selection.selected.includes(selection.current)) return real(selection.selected)
        return real(selection.current ? [selection.current] : [])
    }
    const insertLine = () => {
        const next = addRow(rows)
        const key = rowKey(next[next.length - 1], next.length - 1)
        change(next, "добавление строки")
        select(only(key))
        const first = columns.find((column) => isEditableColumn(column, writable, readOnly))
        if (first) {
            setCurrentColumn(first.name)
            if (cellMode && first.type !== "boolean") setEditing({ key, column: first.name, original: next[next.length - 1], added: true })
        }
    }
    // "Подбор" (1C): the part picks into its editable reference column, with a
    // quantity when it has one; parts with nothing to pick have no command.
    const target = useMemo(() => (editable ? pickTarget(part.columns, writable) : null), [editable, part.columns, writable])
    const pickEndpoint = target?.item.referenceType ? resolveRefEndpoint(target.item.referenceType) : null
    const [pickOpen, setPickOpen] = useState(false)
    const canPick = !!target && !!pickEndpoint && !disabled
    const transferPicked = (cart: CartLine[], merge: boolean) => {
        if (!target) return
        const result = transferPicks(rows, cart, target, merge)
        change(result.rows, "подбор")
        // What the pick touched becomes the selection, so totals and commands apply to it.
        if (result.touched.length) {
            select({ selected: result.touched, current: result.touched[0], anchor: result.touched[0] })
            setCurrentColumn(target.quantity?.name ?? target.item.name)
        }
        const parts = [result.added && `добавлено строк: ${result.added}`, result.merged && `дополнено: ${result.merged}`].filter(Boolean)
        toast.success(`Перенесено позиций: ${cart.length}`, { description: parts.join(", "), action: undoAction })
        if (cellMode) requestAnimationFrame(focusGrid)
    }

    const deleteLines = () => {
        const targets = targetKeys()
        if (targets.size === 0) return
        const first = keys.findIndex((key) => targets.has(key))
        const next = removeRows(rows, targets)
        change(next, targets.size > 1 ? "удаление строк" : "удаление строки")
        const landing = next.length ? rowKey(next[Math.min(first, next.length - 1)], Math.min(first, next.length - 1)) : null
        select(landing ? only(landing) : EMPTY_ROW_SELECTION)
        toast(targets.size > 1 ? `Удалено строк: ${targets.size}` : "Строка удалена", { action: undoAction })
    }
    const duplicateLines = () => {
        if (!writable) return
        const { rows: next, copied } = copyRows(rows, targetKeys(), writable)
        if (copied.length === 0) return
        change(next, "копирование строк")
        select({ selected: copied, current: copied[0], anchor: copied[0] })
    }
    // The selected lines move as a block; the current line alone when it is outside the selection.
    const movedLines = (step: -1 | 1) => moveRows(rows, targetKeys(), step)
    const moveLines = (step: -1 | 1) => {
        const next = movedLines(step)
        if (!next) return
        change(next, "перемещение строк")
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
    // ── Fill (1C "Заполнить", Excel Ctrl+D) ─────────────────────────────
    // The current line's value of the current column goes to the other selected lines.
    const fillTarget = (() => {
        const column = columns.find((col) => col.name === currentColumn)
        const index = selection.current ? keys.indexOf(selection.current) : -1
        const others = [...targetKeys()].filter((key) => key !== selection.current)
        if (!column || index === -1 || !others.length || !isEditableColumn(column, writable, readOnly)) return null
        return { column, index, others }
    })()
    const fillLines = () => {
        if (!fillTarget || disabled) return
        const { column, index, others } = fillTarget
        change(fillColumn(rows, new Set(others), rows[index], column.name), "заполнение колонки")
        toast(`Заполнено строк: ${others.length}`, { description: `«${column.label || column.name}» = ${cellText(rows[index], column) || "пусто"}`, action: undoAction })
    }

    // ── Paste from a spreadsheet ────────────────────────────────────────
    // Lines copied in Excel or Google Sheets land from the current cell (or
    // the cell whose input has focus), adding lines past the end. References
    // are found by code or name; what is not found is reported and left as it was.
    const editableColumn = (column: MetadataField) => isEditableColumn(column, writable, readOnly)
    const resolveReferences = async (column: MetadataField, texts: string[]) => {
        const found = new Map<string, { id: string; name: string }>()
        const endpoint = column.referenceType ? resolveRefEndpoint(column.referenceType) : null
        if (!endpoint) return found
        await Promise.all(texts.map(async (text) => {
            try {
                const result = await apiFetch<CursorListResponse<JsonObject & { id: string }>>(withReferenceListParams(endpoint, { search: text, limit: 10 }))
                const items = (result.items ?? []).filter((item) => item.isFolder !== true)
                const wanted = text.toLowerCase()
                const exact = items.find((item) => [item.code, item.name].some((value) => typeof value === "string" && value.toLowerCase() === wanted))
                const item = exact ?? (items.length === 1 ? items[0] : undefined)
                if (item) found.set(wanted, { id: item.id, name: String(item.name ?? item.code ?? text) })
            } catch { /* not found: reported with the others */ }
        }))
        return found
    }
    const pasteLines = async (text: string, start: { line: number; column: number }) => {
        const grid = parseClipboardGrid(text)
        if (!grid.length) return
        const references = new Map<string, Map<string, { id: string; name: string }>>()
        for (let j = 0; j < Math.max(...grid.map((cells) => cells.length)); j++) {
            const column = columns[start.column + j]
            if (!column || column.type !== "reference" || !editableColumn(column)) continue
            const texts = [...new Set(grid.map((cells) => cells[j]?.trim() ?? "").filter(Boolean))]
            references.set(column.name, await resolveReferences(column, texts))
        }
        const resolve = (column: string, value: string) => references.get(column)?.get(value.toLowerCase()) ?? null
        const result = pasteGrid(rows, start, grid, columns, editableColumn, (column, value) => convertCellText(column, value, resolve),
            () => ({ [ROW_KEY]: newRowKey() }))
        if (!result.touched.length && !result.failures.length) return
        change(result.rows, "вставка")
        if (result.touched.length) select({ selected: result.touched, current: result.touched[0], anchor: result.touched[0] })
        const failed = result.failures.slice(0, 3).map((failure) => `«${failure.text}» (${failure.column})`).join(", ")
        const description = result.failures.length
            ? `Не распознано: ${failed}${result.failures.length > 3 ? ` и ещё ${result.failures.length - 3}` : ""}`
            : undefined
        const message = `Вставлено строк: ${result.touched.length}`
        if (result.failures.length) toast.warning(message, { description, action: undoAction })
        else toast.success(message, { action: undoAction })
    }
    const onGridPaste = (event: React.ClipboardEvent<HTMLDivElement>) => {
        if (!editable || disabled) return
        const text = event.clipboardData.getData("text/plain")
        const target = event.target instanceof Element ? event.target : null
        const inField = target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement
        // A single value pasted into a field stays the field's own paste.
        if (inField && !/[\t\n]/.test(text.replace(/\r?\n$/, ""))) return
        // From a cell (its field or picker) or the grid itself; not from the command bar.
        const cell = target?.closest<HTMLElement>("td[data-field]")
        if (!cell && target !== event.currentTarget) return
        event.preventDefault()
        const field = cell?.dataset.field?.split(".")
        const line = field ? Number(field[1]) : Math.max(0, selection.current ? navKeys.indexOf(selection.current) : 0)
        const column = Math.max(0, columns.findIndex((col) => col.name === (field ? field[2] : currentColumn)))
        void pasteLines(text, { line, column })
    }

    const moveColumn = (step: -1 | 1): boolean => {
        const at = columns.findIndex((column) => column.name === currentColumn)
        const next = at === -1 ? 0 : at + step
        if (next < 0 || next >= columns.length) return false
        setCurrentColumn(columns[next].name)
        if (!selection.current && navKeys[0]) select(only(navKeys[0]))
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
            // Keys typed before the opened cell's editor takes focus are kept
            // and typed into it, so a fast "15" does not lose its "5".
            if (event.target === event.currentTarget && event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
                handled()
                seedRef.current = (seedRef.current ?? "") + event.key
                return
            }
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
        const index = selection.current ? navKeys.indexOf(selection.current) : -1
        const column = columns.find((col) => col.name === currentColumn)
        if (event.metaKey || event.ctrlKey) {
            if (event.code === "KeyZ" && !event.shiftKey) {
                handled()
                undo()
                return
            }
            if (event.code === "KeyD" && !event.shiftKey && editable && !disabled) {
                handled()
                fillLines()
                return
            }
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
        const target = rowMoveTarget(event.key, index, navKeys.length, PAGE_ROWS)
        if (target !== null) {
            handled()
            select(clickRow(selection, navKeys, navKeys[target], { shift: event.shiftKey, toggle: false }))
            if (!currentColumn && columns[0]) setCurrentColumn(columns[0].name)
            return
        }
        // Type-to-edit (Excel, 1C): a printable key on an editable cell opens it
        // with that key typed; a reference cell opens its picker searching for it.
        if (event.key.length === 1 && index !== -1 && column && column.type !== "boolean" && !disabled
            && isEditableColumn(column, writable, readOnly) && !(event.key === " " && column.type === "reference")) {
            handled()
            startEdit(navKeys[index], column, event.key)
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
                startEdit(navKeys[index], column)
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
            case "F7":
                if (canPick) { handled(); setPickOpen(true) }
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
    const statusText = selection.current === ghostKey && showGhost ? `Новая строка · ${linesLabel(rows.length)}` : hasMany
        ? `Выделено ${targetCount} из ${rows.length}`
        : currentIndex !== -1 ? `Строка ${currentIndex + 1} из ${rows.length}` : linesLabel(rows.length)

    // Line context menu (1C): the same commands as the bar, on a right click.
    // A line outside the selection becomes the only selected one first. With
    // inputs in the cells the menu sits on № only, so an input keeps the
    // browser's own menu (paste, spelling).
    const wrapRowInMenu = cellMode || !editable
    const onLineContextMenu = (key: string) => {
        if (!selectedSet.has(key)) setSelection(only(key))
    }
    const lineMenu = (trigger: React.ReactElement) => (
        <ContextMenu>
            <ContextMenuTrigger asChild>{trigger}</ContextMenuTrigger>
            <ContextMenuContent className="w-60" onCloseAutoFocus={(event) => { if (cellMode) { event.preventDefault(); focusGrid() } }}>
                {editable && (
                    <>
                        <ContextMenuItem disabled={disabled} onSelect={insertLine}>
                            <Plus className="mr-2 h-3.5 w-3.5" />Добавить{cellMode && <ContextMenuShortcut>Ins</ContextMenuShortcut>}
                        </ContextMenuItem>
                        {target && pickEndpoint && (
                            <ContextMenuItem disabled={!canPick} onSelect={() => setPickOpen(true)}>
                                <PackagePlus className="mr-2 h-3.5 w-3.5" />Подбор…{cellMode && <ContextMenuShortcut>F7</ContextMenuShortcut>}
                            </ContextMenuItem>
                        )}
                        <ContextMenuItem disabled={disabled || !hasTarget} onSelect={duplicateLines}>
                            <CopyPlus className="mr-2 h-3.5 w-3.5" />{hasMany ? "Скопировать строки" : "Скопировать строку"}{cellMode && <ContextMenuShortcut>F9</ContextMenuShortcut>}
                        </ContextMenuItem>
                        <ContextMenuItem disabled={disabled || !fillTarget} onSelect={fillLines}>
                            <ArrowDownToLine className="mr-2 h-3.5 w-3.5" />Заполнить выделенные{cellMode && <ContextMenuShortcut>Ctrl+D</ContextMenuShortcut>}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem disabled={disabled || !movedLines(-1)} onSelect={() => moveLines(-1)}>
                            <ArrowUp className="mr-2 h-3.5 w-3.5" />Переместить вверх{cellMode && <ContextMenuShortcut>Ctrl+Shift+↑</ContextMenuShortcut>}
                        </ContextMenuItem>
                        <ContextMenuItem disabled={disabled || !movedLines(1)} onSelect={() => moveLines(1)}>
                            <ArrowDown className="mr-2 h-3.5 w-3.5" />Переместить вниз{cellMode && <ContextMenuShortcut>Ctrl+Shift+↓</ContextMenuShortcut>}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem disabled={disabled || !hasTarget} onSelect={deleteLines} className="text-destructive focus:text-destructive">
                            <Trash2 className="mr-2 h-3.5 w-3.5" />{hasMany ? "Удалить строки" : "Удалить строку"}{cellMode && <ContextMenuShortcut>Del</ContextMenuShortcut>}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                    </>
                )}
                <ContextMenuItem disabled={!hasTarget} onSelect={copyToClipboard}>
                    <ClipboardCopy className="mr-2 h-3.5 w-3.5" />Копировать в буфер{cellMode && <ContextMenuShortcut>Ctrl+C</ContextMenuShortcut>}
                </ContextMenuItem>
            </ContextMenuContent>
        </ContextMenu>
    )

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
                onPaste={onGridPaste}
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
                            {target && pickEndpoint && (
                                <CommandButton label={`Подбор: ${target.item.label || target.item.name}`} shortcut={cellMode ? "F7" : undefined} caption="Подбор"
                                    icon={<PackagePlus className="h-3.5 w-3.5" />} disabled={!canPick} onClick={() => setPickOpen(true)} />
                            )}
                            <CommandButton label={hasMany ? "Скопировать строки" : "Скопировать строку"} shortcut={cellMode ? "F9" : undefined}
                                icon={<CopyPlus className="h-3.5 w-3.5" />} disabled={disabled || !hasTarget} onClick={duplicateLines} />
                            <CommandButton label="Заполнить выделенные значением текущей строки" shortcut={cellMode ? "Ctrl+D" : undefined}
                                icon={<ArrowDownToLine className="h-3.5 w-3.5" />} disabled={disabled || !fillTarget} onClick={fillLines} />
                            <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                            <CommandButton label="Переместить вверх" shortcut={cellMode ? "Ctrl+Shift+↑" : undefined}
                                icon={<ArrowUp className="h-3.5 w-3.5" />} disabled={disabled || !movedLines(-1)} onClick={() => moveLines(-1)} />
                            <CommandButton label="Переместить вниз" shortcut={cellMode ? "Ctrl+Shift+↓" : undefined}
                                icon={<ArrowDown className="h-3.5 w-3.5" />} disabled={disabled || !movedLines(1)} onClick={() => moveLines(1)} />
                            <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                            <CommandButton label={hasMany ? "Удалить строки" : "Удалить строку"} shortcut={cellMode ? "Del" : undefined} destructive
                                icon={<Trash2 className="h-3.5 w-3.5" />} disabled={disabled || !hasTarget} onClick={deleteLines} />
                            <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                            <CommandButton label={history.length ? `Отменить: ${history[history.length - 1].label}` : "Отменить"} shortcut={cellMode ? "Ctrl+Z" : undefined}
                                icon={<Undo2 className="h-3.5 w-3.5" />} disabled={disabled || !history.length} onClick={undo} />
                            <span className="ml-auto px-1.5 text-xs tabular-nums text-muted-foreground" aria-live="polite">{statusText}</span>
                        </div>
                    </TooltipProvider>
                )}
                {/* The grid scrolls on its own once it is taller than the screen, as a
                    1C table part does: the column header and the totals stay in view,
                    and sideways scrolling keeps № and the item column in place. */}
                <div ref={scrollRef} className="max-h-[max(18rem,calc(100dvh-15rem))] overflow-auto overscroll-contain">
                <table className={cn("w-full table-fixed border-separate border-spacing-0", isResizing && "select-none")} style={{ minWidth: tableWidth }}>
                    <colgroup>
                        <col style={{ width: NUMBER_COLUMN }} />
                        {/* The last column takes the room left, as in 1C, so the others keep exactly the width they were given. */}
                        {columns.map((column, index) => <col key={column.name} style={{ width: index === columns.length - 1 ? undefined : colWidths[index] }} />)}
                    </colgroup>
                    <thead className="text-xs text-muted-foreground">
                        <tr>
                            <th scope="col" className="sticky left-0 top-0 z-30 border-b bg-muted px-2 py-1.5 text-left font-medium">№</th>
                            {columns.map((column, index) => (
                                <th key={column.name} scope="col"
                                    className={cn("sticky top-0 z-20 truncate border-b bg-muted px-2 py-1.5 text-left font-medium", NUMERIC.has(column.type) && "text-right",
                                        index === 0 && columns.length > 1 && "z-30 border-r",
                                        currentColumn === column.name && "text-foreground")}
                                    style={index === 0 && columns.length > 1 ? { left: NUMBER_COLUMN } : undefined}>
                                    {column.label || column.name}{column.required && editable ? <span className="text-destructive"> *</span> : null}
                                    {index < columns.length - 1 && (
                                        <div aria-hidden title="Перетащите, чтобы изменить ширину; двойной клик — по содержимому"
                                            className="group/resize absolute right-0 top-0 z-10 h-full w-[5px] cursor-col-resize hover:bg-primary/30 active:bg-primary/50"
                                            onMouseDown={(event) => { event.preventDefault(); onResizeStart(index, event) }}
                                            onDoubleClick={(event) => { event.preventDefault(); fitColumn(index) }}>
                                            <div className="absolute right-0 top-1/4 h-1/2 w-px bg-border group-hover/resize:bg-primary/60" />
                                        </div>
                                    )}
                                </th>
                            ))}
                        </tr>
                    </thead>
                    {/* The last line's rule would double the footer's (or the frame's) edge. */}
                    <tbody className="[&>tr:last-child>td]:border-b-0">
                        {rows.length === 0 && !showGhost && (
                            <tr>
                                <td colSpan={columns.length + 1} className="border-b px-3 py-4 text-center text-muted-foreground">
                                    {editable ? (target && pickEndpoint ? "Нет строк. «Добавить» — одну строку, «Подбор» — сразу несколько позиций" : "Нет строк. Добавьте первую командой «Добавить»") : "Нет строк"}
                                </td>
                            </tr>
                        )}
                        {(showGhost ? [...rows, { [ROW_KEY]: ghostKey }] : rows).map((row, index) => {
                            const lineNo = index + 1
                            const key = navKeys[index]
                            const isGhost = key === ghostKey && showGhost
                            const isCurrent = selection.current === key
                            const numberCell = (
                                <td className={cn("sticky left-0 z-10 cursor-pointer select-none border-b bg-inherit px-2 py-0.5 tabular-nums scroll-my-9", rowHeight,
                                    // The accent bar of the current line sits on its frozen № cell, which hides the row's own edge.
                                    isCurrent ? "font-medium text-foreground shadow-[inset_2px_0_0_hsl(var(--ring))]"
                                        : selectedSet.has(key) ? "text-muted-foreground shadow-[inset_2px_0_0_hsl(var(--ring)/0.45)]" : "text-muted-foreground")}
                                    onMouseDown={onCellMouseDown} onClick={(event) => onCellClick(key, null, event)}
                                    onContextMenu={wrapRowInMenu ? undefined : () => onLineContextMenu(key)}
                                    title={isGhost ? "Новая строка: начните вводить в любую ячейку" : undefined}>
                                    {isGhost ? <Plus className="h-3.5 w-3.5 text-muted-foreground/70" aria-label="Новая строка" /> : lineNo}
                                </td>
                            )
                            const rowElement = (
                                <tr key={key} data-selected={selectedSet.has(key) ? "true" : undefined} data-current={isCurrent ? "true" : undefined}
                                    onContextMenu={wrapRowInMenu ? () => onLineContextMenu(key) : undefined}
                                    data-entry-line={isGhost ? "true" : undefined}
                                    className={cn("align-middle", isCurrent ? CURRENT_BG : selectedSet.has(key) ? SELECTED_BG : ROW_BG)}>
                                    {wrapRowInMenu ? numberCell : lineMenu(numberCell)}
                                    {columns.map((column, columnIndex) => {
                                        const cellEditable = isEditableColumn(column, writable, readOnly)
                                        // Every cell: a bottom rule, room for the sticky header and
                                        // footer when revealed, and past the frozen columns sideways.
                                        const frozen = columnIndex === 0 && columns.length > 1
                                        const place = cn("border-b scroll-my-9", frozen && "sticky z-10 border-r bg-inherit")
                                        const placeStyle: React.CSSProperties = frozen ? { left: NUMBER_COLUMN } : { scrollMarginLeft: frozenWidth }
                                        const isCurrentCell = selection.current === key && currentColumn === column.name
                                        const isEditing = cellMode && editingCell === `${key}|${column.name}`
                                        const withTotals = (cell: React.ReactElement) => isCurrentCell && summary && dismissedSummary !== summaryKey ? (
                                            <SelectionSummaryPopover key={column.name} summary={summary} label={column.label || column.name} onDismiss={() => setDismissedSummary(summaryKey)}>
                                                {cell}
                                            </SelectionSummaryPopover>
                                        ) : cell

                                        const missing = !isGhost && cellEditable && column.required && column.type !== "boolean" && isEmptyValue(row[column.name])
                                        const isErrorCell = missing && errorCell === `${key}|${column.name}`
                                        if (cellMode && !isEditing) {
                                            // Cell mode: the value as text; a double click opens it.
                                            return withTotals(
                                                <td key={column.name} data-current-cell={isCurrentCell ? "true" : undefined} data-field={`${part.name}.${index}.${column.name}`}
                                                    title={missing ? `Заполните «${column.label || column.name}»` : undefined} style={placeStyle}
                                                    className={cn(place, rowHeight, "cursor-default select-none truncate px-2 py-0.5",
                                                        NUMERIC.has(column.type) && "text-right tabular-nums",
                                                        !cellEditable && "text-muted-foreground",
                                                        isCurrentCell && CURRENT_CELL, isErrorCell && ERROR_CELL)}
                                                    onMouseDown={onCellMouseDown}
                                                    onClick={(event) => onCellClick(key, column.name, event)}
                                                    onDoubleClick={(event) => { if (!event.shiftKey && !event.metaKey && !event.ctrlKey) startEdit(key, column) }}>
                                                    {column.type === "boolean"
                                                        ? <Checkbox checked={!!row[column.name]} tabIndex={-1} aria-hidden className="pointer-events-none" />
                                                        : missing
                                                            // 1C marks an unfilled required field with a red dashed underline.
                                                            ? <span aria-label="Не заполнено" className="block h-4 border-b border-dashed border-destructive/60" />
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
                                            return withTotals(<td key={column.name} data-current-cell={isCurrentCell ? "true" : undefined} style={placeStyle}
                                                className={cn(place, rowHeight, "truncate px-2 py-0.5 text-muted-foreground", NUMERIC.has(column.type) && "text-right tabular-nums", isCurrentCell && CURRENT_CELL)}
                                                {...selectHandlers}>{cellText(row, column)}</td>)
                                        }
                                        const endpoint = column.type === "reference" && column.referenceType ? resolveRefEndpoint(column.referenceType) : null
                                        return (
                                            withTotals(<td key={column.name} data-current-cell={isCurrentCell ? "true" : undefined} data-editing={isEditing ? "true" : undefined}
                                                style={placeStyle}
                                                className={cn(place, rowHeight, editorHeight, QUIET_EDITORS, "px-0.5 py-0.5", NUMERIC.has(column.type) && "[&_input]:text-right [&_input]:tabular-nums",
                                                    isCurrentCell && CURRENT_CELL, isErrorCell && ERROR_CELL)}
                                                data-field={`${part.name}.${index}.${column.name}`} {...selectHandlers}>
                                                {endpoint ? (
                                                    <ReferenceField
                                                        value={typeof row[column.name] === "string" ? String(row[column.name]) : ""}
                                                        displayName={referenceDisplayName(row, column.name)}
                                                        onChange={(value, display) => {
                                                            updateCell(index, column.name, value, display)
                                                            if (isEditing) {
                                                                // Picking a value finishes the cell; focus returns to the
                                                                // grid once the picker has handed it back.
                                                                setEditing(null)
                                                                requestAnimationFrame(() => requestAnimationFrame(focusGrid))
                                                            }
                                                        }}
                                                        apiEndpoint={endpoint}
                                                        placeholder={isGhost ? "" : "Выберите…"}
                                                        compact
                                                        disabled={disabled}
                                                    />
                                                ) : CALC_TYPES.has(column.type) ? (
                                                    <NumberCellInput value={row[column.name]} integer={column.type === "integer"} disabled={disabled}
                                                        // A decimal the server sent as text stays text (the wire contract), a number stays a number.
                                                        onValue={(value) => updateCell(index, column.name, typeof row[column.name] === "string" ? String(value) : value)} />
                                                ) : (
                                                    renderInput(column, row[column.name], (value) => updateCell(index, column.name, value))
                                                )}
                                            </td>)
                                        )
                                    })}
                                </tr>
                            )
                            return wrapRowInMenu ? <React.Fragment key={key}>{lineMenu(rowElement)}</React.Fragment> : rowElement
                        })}
                    </tbody>
                    {hasFooter && (
                        <tfoot className="text-sm font-medium">
                            <tr>
                                <td className="sticky bottom-0 left-0 z-30 border-t bg-muted px-2 py-1.5 text-xs font-normal text-muted-foreground">Итого</td>
                                {columns.map((column, index) => (
                                    <td key={column.name} className={cn("sticky bottom-0 z-20 border-t bg-muted px-1 py-1.5", index === 0 && columns.length > 1 && "z-30 border-r")}
                                        style={index === 0 && columns.length > 1 ? { left: NUMBER_COLUMN } : undefined}>
                                        {footer[index] ? <FooterTotal summary={footer[index]} /> : null}
                                    </td>
                                ))}
                            </tr>
                        </tfoot>
                    )}
                </table>
                </div>
            </div>
            {target && pickEndpoint && pickOpen && (
                <TablePartPickDialog open={pickOpen} onOpenChange={(open) => { setPickOpen(open); if (!open && cellMode) requestAnimationFrame(focusGrid) }}
                    title={title} target={target} endpoint={pickEndpoint}
                    inDocument={documentQuantities(rows, target)} onTransfer={transferPicked} />
            )}
        </section>
    )
}

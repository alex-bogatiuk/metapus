"use client"

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { AlertCircle, ArrowDown, ArrowDownToLine, ArrowUp, ArrowUpDown, ChevronDown, ChevronUp, ClipboardCopy, Columns3, CopyPlus, PackagePlus, PanelRight, Plus, Search, Trash2, Undo2, X } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuShortcut, ContextMenuTrigger } from "@/components/ui/context-menu"
import {
    DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator,
    DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { TablePartLinePanel } from "@/components/shared/table-part-line-panel"
import { useRowMotion } from "@/hooks/useRowMotion"
import { SCAN_TARGET_ATTR, useBarcodeScan } from "@/hooks/useBarcodeScan"
import { ReferenceField } from "@/components/shared/reference-field"
import { OVERLAY_SELECTOR } from "@/components/shared/data-table"
import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { resolveRefEndpoint } from "@/lib/reference-endpoint"
import { amountUnitPairs, displayValue } from "@/lib/form-layout"
import { EMPTY_ROW_SELECTION, clickRow, keepExisting, rowMoveTarget, summaryRows, type RowSelection } from "@/lib/row-selection"
import { fieldSummaryExtractor, formatSummaryNumber, isSummableField, summarize, summaryCopyText, summaryGroupText, type SelectionSummary } from "@/lib/selection-summary"
import { SelectionSummaryPopover } from "@/components/shared/selection-summary-popover"
import { copyText } from "@/lib/clipboard"
import { useCompactMode, useTablePartCellMode } from "@/hooks/useCompactMode"
import { TablePartPickDialog } from "@/components/shared/table-part-pick-dialog"
import { documentQuantities, pickTarget, transferPicks, type CartLine } from "@/lib/pick-lines"
import { useColumnResize } from "@/hooks/useColumnResize"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { cn } from "@/lib/utils"
import {
    LINE_IDENTITY_COLUMNS, ROW_KEY, addRow, copyRows, fillColumn, findCells, isEditableColumn, missingCells, moveRows, newRowKey,
    referenceDisplayName, removeRows, restoreRow, rowKey, setCell, sortRows, type LineCell,
} from "@/lib/table-part-rows"
import { convertCellText, evaluateNumberInput, moneyFromNumber, moneyText, parseClipboardGrid, parseNumberText, pasteGrid } from "@/lib/grid-paste"
import type { DocumentCurrency } from "@/hooks/useDocumentCurrency"
import { applyFormulas, formulaLabel, hasFormulas } from "@/lib/formula"
import { apiFetch } from "@/lib/api"
import { withReferenceListParams } from "@/lib/reference-utils"
import type { CursorListResponse } from "@/types/common"
import { isImeComposing } from "@/lib/keyboard-utils"

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
    /**
     * The document's currency: money reads and is typed in its major units
     * (stored as minor units). Null keeps minor units (no currency known).
     */
    currency?: DocumentCurrency | null
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

/**
 * Number columns get the grid's own editor (calculator, "+5"). Money is
 * entered as minor units, as in the form, but without the form's hint
 * paragraph, which has no room in a grid line.
 */
const CALC_TYPES = new Set(["integer", "number", "decimal", "money"])
/** Item fields a scanned code is matched against, the most specific first. */
const SCAN_FIELDS = ["barcode", "ean", "gtin", "code", "sku", "article", "vendorCode"]
/** Missing-value mark of a cell with an input (cell mode draws its own): a red dashed rule, as in 1C. */
const MISSING_MARK = "after:pointer-events-none after:absolute after:inset-x-2 after:bottom-[3px] after:border-b after:border-dashed after:border-destructive/60 after:content-['']"

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
function NumberCellInput({ value, integer, disabled, money = null, onValue }: {
    value: JsonValue | undefined
    integer: boolean
    disabled: boolean
    /** Money in a currency with this many places: shown and typed in major units. */
    money?: number | null
    onValue: (value: number) => void
}) {
    const [draft, setDraft] = useState<string | null>(null)
    const [start, setStart] = useState<number | null>(null)
    const shown = money !== null ? moneyText(value, money)
        : value === undefined || value === null || value === "" ? "" : String(value)
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
                // Keys that confirm an IME candidate belong to the IME, not the grid.
                if (isImeComposing(event)) {
                    event.stopPropagation()
                    return
                }
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

function cellText(row: JsonObject, column: MetadataField, currency?: DocumentCurrency | null): string {
    if (column.type === "reference") return referenceDisplayName(row, column.name) ?? String(row[column.name] ?? "")
    if (column.type === "money" && currency) {
        // "1 999,00": major units of the document's currency, grouped like the totals.
        const major = moneyText(row[column.name], currency.scale)
        return major ? formatSummaryNumber(major.replace(",", ".")) : ""
    }
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
export function TablePartEditor({ part, rows, writable, readOnly, disabled, hiddenColumns, bare = false, onChange, renderInput, widthsKey, focusRequest, currency = null }: TablePartEditorProps) {
    const cellMode = useTablePartCellMode()
    const compact = useCompactMode()
    // 32 px lines (28 in compact mode): editors 28/24 px high in 2 px of padding.
    const rowHeight = compact ? "h-7" : "h-8"
    const editorHeight = compact ? "[&_input]:h-6" : "[&_input]:h-7"
    const allColumns = useMemo(
        () => part.columns.filter((column) => !hiddenColumns.has(column.name) && !LINE_IDENTITY_COLUMNS.has(column.name)),
        [part.columns, hiddenColumns],
    )
    // The columns the user keeps in the grid ("Колонки"); the line panel shows all.
    const visibleKey = widthsKey ? `${widthsKey}__columns` : ""
    const shownNames = useUserPrefsStore((state) => (visibleKey ? state.listColumns[visibleKey] : undefined))
    const saveShown = useUserPrefsStore((state) => state.setListColumns)
    const columns = useMemo(
        () => (shownNames?.length ? allColumns.filter((column) => shownNames.includes(column.name)) : allColumns),
        [allColumns, shownNames],
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
        // One document's lines share its currency: money adds up in it (in
        // minor units while the currency is not known).
        const shared = currency ? { decimalPlaces: currency.scale, symbol: currency.code } : true as const
        return new Map(part.columns.map((column) => [column.name, fieldSummaryExtractor(column, column.name, units.get(column.name), shared)]))
    }, [part.columns, currency])
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
    const sectionRef = useRef<HTMLElement>(null)
    const { flash } = useRowMotion(scrollRef, navKeys)
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
    // Calculated columns (meta:"calc:...") follow their inputs on every change,
    // as 1C recalculates a line's amount; the server stores its own result.
    const calculated = hasFormulas(part.columns)
    const change = (next: JsonObject[], label: string, cell?: string) => {
        const top = history[history.length - 1]
        if (!(cell && top?.cell === cell)) setHistory([...history, { before: rows, label, cell }].slice(-HISTORY_LIMIT))
        onChange(calculated ? next.map((row) => applyFormulas(row, part.columns)) : next)
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
            flash(result.touched)
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
        flash(copied)
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
                .map((row) => columns.map((column) => cellText(row, column, currency)).join("\t"))
                .join("\n")
        } else {
            const index = selection.current ? keys.indexOf(selection.current) : -1
            const column = columns.find((col) => col.name === currentColumn)
            if (index !== -1 && column) text = cellText(rows[index], column, currency)
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
        flash(others)
        toast(`Заполнено строк: ${others.length}`, { description: `«${column.label || column.name}» = ${cellText(rows[index], column, currency) || "пусто"}`, action: undoAction })
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
        const result = pasteGrid(rows, start, grid, columns, editableColumn, (column, value) => convertCellText(column, value, resolve, currency?.scale ?? null),
            () => ({ [ROW_KEY]: newRowKey() }))
        if (!result.touched.length && !result.failures.length) return
        change(result.rows, "вставка")
        if (result.touched.length) {
            select({ selected: result.touched, current: result.touched[0], anchor: result.touched[0] })
            flash(result.touched)
        }
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
                if (event.key === "Enter" && event.shiftKey) {
                    if (selection.current && selection.current !== ghostKey) { handled(); openLine() }
                    return
                }
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

    // ── Missing values as the user works (1C red underline) ─────────────
    // Required cells left empty are marked in both modes; the bar counts them
    // and takes the user to the next one.
    const gaps: LineCell[] = editable ? missingCells(rows, columns, editableColumn) : []
    /** Makes a cell current and brings it into view; with inputs in the cells, its input takes focus. */
    const goToCell = (cell: LineCell, focusInput = !cellMode) => {
        select(only(cell.key))
        setCurrentColumn(cell.column)
        if (cellMode) focusGrid()
        if (focusInput) {
            requestAnimationFrame(() => gridRef.current?.querySelector<HTMLElement>(`td[data-field="${CSS.escape(`${part.name}.${cell.line}.${cell.column}`)}"]`)
                ?.querySelector<HTMLElement>("input, textarea, select, button")?.focus())
        }
    }
    const nextGap = () => {
        if (!gaps.length) return
        const here = selection.current ? keys.indexOf(selection.current) : -1
        const at = gaps.findIndex((cell) => cell.line > here || (cell.line === here && columns.findIndex((c) => c.name === cell.column) > columns.findIndex((c) => c.name === currentColumn)))
        goToCell(gaps[at === -1 ? 0 : at])
    }

    // ── Search in the part (Ctrl/Cmd+F) ──────────────────────────────────
    const [searchOpen, setSearchOpen] = useState(false)
    const [query, setQuery] = useState("")
    const [matchIndex, setMatchIndex] = useState(0)
    const searchRef = useRef<HTMLInputElement>(null)
    const textOf = (row: JsonObject, column: MetadataField) => cellText(row, column, currency)
    const matches = searchOpen ? findCells(rows, columns, query, textOf) : []
    const currentMatch = matches.length ? matches[Math.min(matchIndex, matches.length - 1)] : null
    const matchState = (key: string, column: string) => !matches.length ? undefined
        : currentMatch?.key === key && currentMatch.column === column ? "current"
            : matches.some((cell) => cell.key === key && cell.column === column) ? "true" : undefined
    const openSearch = () => {
        setSearchOpen(true)
        requestAnimationFrame(() => { searchRef.current?.focus(); searchRef.current?.select() })
    }
    const closeSearch = () => {
        setSearchOpen(false)
        setQuery("")
        if (cellMode) focusGrid()
    }
    const showMatch = (list: LineCell[], at: number) => {
        if (!list.length) return
        const index = (at + list.length) % list.length
        setMatchIndex(index)
        goToCell(list[index], false)
        // The search box keeps the keyboard: Enter goes on to the next match.
        requestAnimationFrame(() => searchRef.current?.focus())
    }

    // ── Ordering (1C "Упорядочить"): changes the lines' order ────────────
    const orderBy = (column: MetadataField, direction: "asc" | "desc") => {
        const next = sortRows(rows, column, direction, textOf)
        change(next, `упорядочивание по «${column.label || column.name}»`)
        toast(`Строки упорядочены по «${column.label || column.name}»`, { action: undoAction })
    }

    // ── Columns the grid shows ────────────────────────────────────────────
    const shown = new Set(columns.map((column) => column.name))
    const toggleColumn = (column: MetadataField, visible: boolean) => {
        if (!visibleKey) return
        const next = allColumns.filter((c) => (c.name === column.name ? visible : shown.has(c.name))).map((c) => c.name)
        saveShown(visibleKey, next.length === allColumns.length ? [] : next)
    }
    /** A column the user must fill stays in the grid, so its gaps can be seen. */
    const pinnedColumn = (column: MetadataField) => (column.required && editableColumn(column)) || (shown.has(column.name) && shown.size === 1)

    // ── Line details panel ───────────────────────────────────────────────
    const [panelOpen, setPanelOpen] = useState(false)
    const openLine = (key = selection.current) => {
        if (!key || key === ghostKey || !keys.includes(key)) return
        if (key !== selection.current) select(only(key))
        setPanelOpen(true)
    }
    const panelLine = selection.current ? keys.indexOf(selection.current) : -1

    // ── Barcode scanner ──────────────────────────────────────────────────
    // A scan finds the item by barcode or code and adds one to its line (or a
    // new line), wherever the keyboard is on the page.
    const canScan = editable && !disabled && !!target && !!pickEndpoint
    const applyScan = (item: { id: string; name: string; code?: string }) => {
        if (!target) return
        const result = transferPicks(rows, [{ ...item, quantity: "1" }], target, true)
        change(result.rows, "сканирование")
        if (result.touched.length) {
            select(only(result.touched[0]))
            setCurrentColumn(target.quantity?.name ?? target.item.name)
            flash(result.touched)
        }
        toast.success(`${item.name}: +1`, { duration: 1500 })
    }
    const applyScanRef = useRef(applyScan)
    useEffect(() => { applyScanRef.current = applyScan })
    useBarcodeScan(sectionRef, canScan, (code) => {
        // Whatever the scan typed into an open cell is taken back. Its first key
        // may have opened a cell this render has not seen yet, so the cell is
        // closed unconditionally; nothing reached it while its keys were held back.
        seedRef.current = null
        if (editing) finishEdit(true)
        else setEditing(null)
        if (cellMode) requestAnimationFrame(focusGrid)
        if (!pickEndpoint) return
        void apiFetch<CursorListResponse<JsonObject & { id: string }>>(withReferenceListParams(pickEndpoint, { search: code, limit: 10 }))
            .then((result) => {
                const wanted = code.trim().toLowerCase()
                const item = (result.items ?? []).filter((candidate) => candidate.isFolder !== true)
                    .map((candidate) => ({ candidate, rank: SCAN_FIELDS.findIndex((field) => {
                        const value = candidate[field]
                        return typeof value === "string" && value.trim().toLowerCase() === wanted
                    }) }))
                    .filter(({ rank }) => rank !== -1)
                    .sort((a, b) => a.rank - b.rank)[0]?.candidate
                if (!item) {
                    toast.error(`Штрихкод «${code}» не найден`)
                    return
                }
                applyScanRef.current({ id: item.id, name: String(item.name ?? item.code ?? code), code: typeof item.code === "string" ? item.code : undefined })
            })
            .catch(() => toast.error(`Не удалось найти «${code}»`))
    })

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
                <ContextMenuItem disabled={panelLine === -1} onSelect={() => openLine()}>
                    <PanelRight className="mr-2 h-3.5 w-3.5" />Открыть строку{cellMode && <ContextMenuShortcut>Shift+Enter</ContextMenuShortcut>}
                </ContextMenuItem>
                <ContextMenuItem disabled={!hasTarget} onSelect={copyToClipboard}>
                    <ClipboardCopy className="mr-2 h-3.5 w-3.5" />Копировать в буфер{cellMode && <ContextMenuShortcut>Ctrl+C</ContextMenuShortcut>}
                </ContextMenuItem>
            </ContextMenuContent>
        </ContextMenu>
    )

    /** The editor of a cell: a picker, the number editor or the form's input. Shared by the grid and the line panel. */
    const renderEditor = (column: MetadataField, row: JsonObject, index: number, options: { compact: boolean; isEditing?: boolean; isGhost?: boolean }) => {
        const endpoint = column.type === "reference" && column.referenceType ? resolveRefEndpoint(column.referenceType) : null
        if (endpoint) {
            return (
                <ReferenceField
                    value={typeof row[column.name] === "string" ? String(row[column.name]) : ""}
                    displayName={referenceDisplayName(row, column.name)}
                    onChange={(value, display) => {
                        updateCell(index, column.name, value, display)
                        if (options.isEditing) {
                            // Picking a value finishes the cell; focus returns to the
                            // grid once the picker has handed it back.
                            setEditing(null)
                            requestAnimationFrame(() => requestAnimationFrame(focusGrid))
                        }
                    }}
                    apiEndpoint={endpoint}
                    placeholder={options.isGhost ? "" : "Выберите…"}
                    compact={options.compact}
                    disabled={disabled}
                />
            )
        }
        if (CALC_TYPES.has(column.type)) {
            return (
                <NumberCellInput value={row[column.name]} disabled={disabled}
                    integer={column.type === "integer" || (column.type === "money" && (currency?.scale ?? 0) === 0)}
                    money={column.type === "money" ? currency?.scale ?? null : null}
                    // Money travels as a string of minor units; a decimal the server sent as text stays text.
                    onValue={(value) => updateCell(index, column.name, column.type === "money"
                        ? (currency ? moneyFromNumber(value, currency.scale) : String(Math.round(value)))
                        : typeof row[column.name] === "string" ? String(value) : value)} />
            )
        }
        return renderInput(column, row[column.name], (value) => updateCell(index, column.name, value))
    }

    const onGridBlur = (event: React.FocusEvent<HTMLDivElement>) => {
        // Focus leaving for outside the grid closes the cell; a picker's popover is still the cell's.
        if (!editing) return
        const next = event.relatedTarget
        if (next instanceof Element && (event.currentTarget.contains(next) || next.closest(OVERLAY_SELECTOR))) return
        setEditing(null)
    }

    return (
        <section ref={sectionRef} className={bare ? "space-y-2" : "mt-8 space-y-2"} aria-label={title} data-table-part={part.name}
            {...(canScan ? { [SCAN_TARGET_ATTR]: "" } : {})}>
            {!bare && (
                <div className="flex items-baseline gap-2">
                    <h3 className="text-sm font-semibold">{title}</h3>
                    <span className="text-xs tabular-nums text-muted-foreground">{rows.length || ""}</span>
                </div>
            )}
            <div
                ref={gridRef}
                // overflow-clip, not hidden: revealing a cell must scroll the grid, never shift the frame.
                className={cn("overflow-clip rounded-md border bg-card text-sm shadow-control", cellMode && "outline-none focus-visible:ring-1 focus-visible:ring-ring")}
                onPaste={onGridPaste}
                onKeyDownCapture={(event) => {
                    // Ctrl/Cmd+F searches the part, from a cell's input as from the grid.
                    if ((event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey && event.code === "KeyF" && editable) {
                        event.preventDefault()
                        event.stopPropagation()
                        openSearch()
                    }
                }}
                {...(cellMode ? { tabIndex: 0, onKeyDown: onGridKeyDown, onBlur: onGridBlur, "data-cell-mode": "true" } : {})}
            >
                {editable && (
                    // 1C command bar: line commands sit between the title and the grid
                    // and act on the selected lines (else the current one).
                    <TooltipProvider delayDuration={400}>
                        <div role="toolbar" aria-label={`Команды: ${title}`}
                            className="flex flex-wrap items-center gap-0.5 gap-y-1 border-b bg-muted/30 px-1.5 py-1">
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
                            <DropdownMenu>
                                <Tooltip>
                                    <TooltipTrigger asChild>
                                        <DropdownMenuTrigger asChild>
                                            <Button type="button" variant="ghost" size="sm" className="h-7 w-7 px-0" aria-label="Упорядочить строки"
                                                disabled={disabled || rows.length < 2} onMouseDown={(event) => event.preventDefault()}>
                                                <ArrowUpDown className="h-3.5 w-3.5" />
                                            </Button>
                                        </DropdownMenuTrigger>
                                    </TooltipTrigger>
                                    <TooltipContent side="bottom">Упорядочить строки по колонке</TooltipContent>
                                </Tooltip>
                                <DropdownMenuContent align="start" className="w-56" onCloseAutoFocus={(event) => { if (cellMode) { event.preventDefault(); focusGrid() } }}>
                                    <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">Упорядочить строки по</DropdownMenuLabel>
                                    {columns.map((column) => (
                                        <DropdownMenuSub key={column.name}>
                                            <DropdownMenuSubTrigger>{column.label || column.name}</DropdownMenuSubTrigger>
                                            <DropdownMenuSubContent>
                                                <DropdownMenuItem onSelect={() => orderBy(column, "asc")}>По возрастанию</DropdownMenuItem>
                                                <DropdownMenuItem onSelect={() => orderBy(column, "desc")}>По убыванию</DropdownMenuItem>
                                            </DropdownMenuSubContent>
                                        </DropdownMenuSub>
                                    ))}
                                </DropdownMenuContent>
                            </DropdownMenu>
                            <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                            <CommandButton label={hasMany ? "Удалить строки" : "Удалить строку"} shortcut={cellMode ? "Del" : undefined} destructive
                                icon={<Trash2 className="h-3.5 w-3.5" />} disabled={disabled || !hasTarget} onClick={deleteLines} />
                            <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                            <CommandButton label={history.length ? `Отменить: ${history[history.length - 1].label}` : "Отменить"} shortcut={cellMode ? "Ctrl+Z" : undefined}
                                icon={<Undo2 className="h-3.5 w-3.5" />} disabled={disabled || !history.length} onClick={undo} />
                            <CommandButton label="Открыть строку: все её поля" shortcut={cellMode ? "Shift+Enter" : undefined}
                                icon={<PanelRight className="h-3.5 w-3.5" />} disabled={panelLine === -1} onClick={() => openLine()} />
                            <div className="ml-auto flex items-center gap-0.5">
                                {gaps.length > 0 && !searchOpen && (
                                    <button type="button" onMouseDown={(event) => event.preventDefault()} onClick={nextGap}
                                        title="Перейти к следующей незаполненной ячейке"
                                        className="flex h-7 items-center gap-1 rounded px-1.5 text-xs text-destructive hover:bg-destructive/10 animate-badge-in">
                                        <AlertCircle className="h-3.5 w-3.5" />
                                        <span className="tabular-nums">Не заполнено: {gaps.length}</span>
                                    </button>
                                )}
                                {searchOpen ? (
                                    <div className="flex items-center gap-0.5 animate-in fade-in-0 slide-in-from-right-1 duration-150 motion-reduce:animate-none">
                                        <div className="relative">
                                            <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
                                            <input ref={searchRef} value={query} aria-label={`Поиск: ${title}`} placeholder="Найти в строках"
                                                className="h-7 w-44 rounded-md border bg-background pl-6 pr-2 text-xs outline-none focus:border-ring"
                                                onChange={(event) => {
                                                    setQuery(event.target.value)
                                                    const found = findCells(rows, columns, event.target.value, textOf)
                                                    setMatchIndex(0)
                                                    if (found.length) goToCell(found[0], false)
                                                }}
                                                onKeyDown={(event) => {
                                                    if (event.key === "Enter" || event.key === "ArrowDown" || event.key === "ArrowUp") {
                                                        event.preventDefault()
                                                        event.stopPropagation()
                                                        showMatch(matches, matchIndex + (event.shiftKey || event.key === "ArrowUp" ? -1 : 1))
                                                    } else if (event.key === "Escape") {
                                                        event.preventDefault()
                                                        event.stopPropagation()
                                                        closeSearch()
                                                    }
                                                }} />
                                        </div>
                                        <span className="w-14 text-center text-xs tabular-nums text-muted-foreground" aria-live="polite">
                                            {matches.length ? `${Math.min(matchIndex, matches.length - 1) + 1} из ${matches.length}` : query.trim() ? "нет" : ""}
                                        </span>
                                        <Button type="button" variant="ghost" size="icon" className="h-7 w-6" aria-label="Предыдущее совпадение" disabled={!matches.length}
                                            onMouseDown={(event) => event.preventDefault()} onClick={() => showMatch(matches, matchIndex - 1)}><ChevronUp className="h-3.5 w-3.5" /></Button>
                                        <Button type="button" variant="ghost" size="icon" className="h-7 w-6" aria-label="Следующее совпадение" disabled={!matches.length}
                                            onMouseDown={(event) => event.preventDefault()} onClick={() => showMatch(matches, matchIndex + 1)}><ChevronDown className="h-3.5 w-3.5" /></Button>
                                        <Button type="button" variant="ghost" size="icon" className="h-7 w-6" aria-label="Закрыть поиск"
                                            onMouseDown={(event) => event.preventDefault()} onClick={closeSearch}><X className="h-3.5 w-3.5" /></Button>
                                    </div>
                                ) : (
                                    <CommandButton label="Найти в строках" shortcut="Ctrl+F" icon={<Search className="h-3.5 w-3.5" />} disabled={!rows.length} onClick={openSearch} />
                                )}
                                {visibleKey && (
                                    <DropdownMenu>
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <DropdownMenuTrigger asChild>
                                                    <Button type="button" variant="ghost" size="sm" className="h-7 w-7 px-0" aria-label="Колонки"
                                                        onMouseDown={(event) => event.preventDefault()}>
                                                        <Columns3 className="h-3.5 w-3.5" />
                                                    </Button>
                                                </DropdownMenuTrigger>
                                            </TooltipTrigger>
                                            <TooltipContent side="bottom">Колонки таблицы</TooltipContent>
                                        </Tooltip>
                                        <DropdownMenuContent align="end" className="w-56" onCloseAutoFocus={(event) => { if (cellMode) { event.preventDefault(); focusGrid() } }}>
                                            <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">Показывать в таблице</DropdownMenuLabel>
                                            {allColumns.map((column) => (
                                                <DropdownMenuCheckboxItem key={column.name} checked={shown.has(column.name)} disabled={pinnedColumn(column)}
                                                    onSelect={(event) => event.preventDefault()} onCheckedChange={(checked) => toggleColumn(column, checked === true)}>
                                                    {column.label || column.name}
                                                </DropdownMenuCheckboxItem>
                                            ))}
                                            <DropdownMenuSeparator />
                                            <p className="px-2 py-1 text-[11px] text-muted-foreground">Скрытые поля видны в панели строки (Shift+Enter)</p>
                                        </DropdownMenuContent>
                                    </DropdownMenu>
                                )}
                                {!searchOpen && <span className="whitespace-nowrap px-1.5 text-xs tabular-nums text-muted-foreground" aria-live="polite">{statusText}</span>}
                            </div>
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
                                    {column.type === "money" && currency?.code && <span className="ml-1 font-normal text-muted-foreground/70">{currency.code}</span>}
                                    {column.calc && (
                                        <span className="ml-1 font-serif italic text-muted-foreground/70" title={`Рассчитывается: ${formulaLabel(column.calc, part.columns)}`}
                                            aria-label={`рассчитывается: ${formulaLabel(column.calc, part.columns)}`}>ƒ</span>
                                    )}
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
                                    onDoubleClick={() => openLine(key)}
                                    onContextMenu={wrapRowInMenu ? undefined : () => onLineContextMenu(key)}
                                    title={isGhost ? "Новая строка: начните вводить в любую ячейку" : undefined}>
                                    {isGhost ? <Plus className="h-3.5 w-3.5 text-muted-foreground/70" aria-label="Новая строка" /> : lineNo}
                                </td>
                            )
                            const rowElement = (
                                <tr key={key} data-row-key={key} data-selected={selectedSet.has(key) ? "true" : undefined} data-current={isCurrent ? "true" : undefined}
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
                                                    data-match={matchState(key, column.name)}
                                                    title={missing ? `Заполните «${column.label || column.name}»` : undefined} style={placeStyle}
                                                    className={cn(place, rowHeight, "cursor-default select-none truncate px-2 py-0.5",
                                                        NUMERIC.has(column.type) && "text-right tabular-nums",
                                                        // Server values read muted; a calculated value is data and reads as such.
                                                        !cellEditable && !column.calc && "text-muted-foreground",
                                                        isCurrentCell && CURRENT_CELL, isErrorCell && ERROR_CELL)}
                                                    onMouseDown={onCellMouseDown}
                                                    onClick={(event) => onCellClick(key, column.name, event)}
                                                    onDoubleClick={(event) => { if (!event.shiftKey && !event.metaKey && !event.ctrlKey) startEdit(key, column) }}>
                                                    {column.type === "boolean"
                                                        ? <Checkbox checked={!!row[column.name]} tabIndex={-1} aria-hidden className="pointer-events-none" />
                                                        : missing
                                                            // 1C marks an unfilled required field with a red dashed underline.
                                                            ? <span aria-label="Не заполнено" className="block h-4 border-b border-dashed border-destructive/60" />
                                                            : cellText(row, column, currency)}
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
                                                data-match={matchState(key, column.name)}
                                                className={cn(place, rowHeight, "truncate px-2 py-0.5", !column.calc && "text-muted-foreground", NUMERIC.has(column.type) && "text-right tabular-nums", isCurrentCell && CURRENT_CELL)}
                                                {...selectHandlers}>{cellText(row, column, currency)}</td>)
                                        }
                                        return (
                                            withTotals(<td key={column.name} data-current-cell={isCurrentCell ? "true" : undefined} data-editing={isEditing ? "true" : undefined}
                                                style={placeStyle} data-match={matchState(key, column.name)}
                                                title={missing ? `Заполните «${column.label || column.name}»` : undefined}
                                                className={cn(place, rowHeight, editorHeight, QUIET_EDITORS, "px-0.5 py-0.5", NUMERIC.has(column.type) && "[&_input]:text-right [&_input]:tabular-nums",
                                                    // Inputs get the same red dashed mark of a required gap as text cells.
                                                    missing && !isEditing && MISSING_MARK, missing && !isEditing && !frozen && "relative",
                                                    isCurrentCell && CURRENT_CELL, isErrorCell && ERROR_CELL)}
                                                data-field={`${part.name}.${index}.${column.name}`} {...selectHandlers}>
                                                {renderEditor(column, row, index, { compact: true, isEditing, isGhost })}
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
            {panelOpen && panelLine !== -1 && (
                <TablePartLinePanel open={panelOpen} onOpenChange={(open) => { setPanelOpen(open); if (!open && cellMode) requestAnimationFrame(focusGrid) }}
                    title={title} columns={allColumns} line={panelLine} lineCount={rows.length}
                    onNavigate={(line) => select(only(keys[line]))}
                    renderField={(column) => isEditableColumn(column, writable, readOnly)
                        ? renderEditor(column, rows[panelLine], panelLine, { compact: false })
                        : <p className={cn("min-h-8 py-1.5 text-sm", NUMERIC.has(column.type) && "tabular-nums")}>
                            {cellText(rows[panelLine], column, currency) || "—"}
                            {column.calc && <span className="ml-1.5 font-serif text-xs italic text-muted-foreground" title={formulaLabel(column.calc, part.columns)}>ƒ</span>}
                        </p>} />
            )}
            {target && pickEndpoint && pickOpen && (
                <TablePartPickDialog open={pickOpen} onOpenChange={(open) => { setPickOpen(open); if (!open && cellMode) requestAnimationFrame(focusGrid) }}
                    title={title} target={target} endpoint={pickEndpoint}
                    inDocument={documentQuantities(rows, target)} onTransfer={transferPicked} />
            )}
        </section>
    )
}

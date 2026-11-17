"use client"

import React, { useMemo, useState } from "react"
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { ReferenceField } from "@/components/shared/reference-field"
import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { resolveRefEndpoint } from "@/lib/reference-endpoint"
import { amountUnitPairs } from "@/lib/form-layout"
import { EMPTY_ROW_SELECTION, clickRow, keepExisting, summaryRows, type RowSelection } from "@/lib/row-selection"
import { fieldSummaryExtractor, summarize } from "@/lib/selection-summary"
import { SelectionSummaryPopover } from "@/components/shared/selection-summary-popover"
import { cn } from "@/lib/utils"
import {
    LINE_IDENTITY_COLUMNS, addRow, isEditableColumn, moveRow, referenceDisplayName, removeRow, rowKey, setCell,
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

function cellText(row: JsonObject, column: MetadataField): string {
    if (column.type === "reference") return referenceDisplayName(row, column.name) ?? String(row[column.name] ?? "")
    if (column.type === "boolean") return row[column.name] ? "Да" : "Нет"
    const value = row[column.name]
    return value === undefined || value === null ? "" : String(value)
}

/**
 * A document table part as a grid: № from the position, one column per line
 * field. A writable part adds, removes and reorders lines; a cell is editable
 * when the line request accepts its field.
 */
export function TablePartEditor({ part, rows, writable, readOnly, disabled, hiddenColumns, bare = false, onChange, renderInput }: TablePartEditorProps) {
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
    }

    return (
        <section className={bare ? "space-y-2" : "mt-8 space-y-2"} aria-label={title} data-table-part={part.name}>
            <div className={bare && !editable ? "hidden" : "flex items-center justify-between gap-2"}>
                {bare ? <span /> : <Label className="text-sm font-semibold">{title}</Label>}
                {editable && (
                    <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={() => onChange(addRow(rows))}>
                        <Plus className="h-3.5 w-3.5" />
                        Добавить строку
                    </Button>
                )}
            </div>
            <div className="overflow-x-auto rounded border text-sm">
                <table className="w-full border-collapse">
                    <thead className="bg-muted/50">
                        <tr className="border-b">
                            <th scope="col" className="w-10 px-2 py-2 text-left font-medium">№</th>
                            {columns.map((column) => (
                                <th key={column.name} scope="col" className="min-w-32 px-2 py-2 text-left font-medium">
                                    {column.label || column.name}{column.required && editable ? <span className="text-destructive"> *</span> : null}
                                </th>
                            ))}
                            {editable && <th scope="col" className="w-24 px-2 py-2"><span className="sr-only">Действия со строкой</span></th>}
                        </tr>
                    </thead>
                    <tbody>
                        {rows.length === 0 && (
                            <tr>
                                <td colSpan={columns.length + (editable ? 2 : 1)} className="px-3 py-4 text-center text-muted-foreground">Нет строк</td>
                            </tr>
                        )}
                        {rows.map((row, index) => {
                            const lineNo = index + 1
                            return (
                                <tr key={keys[index]} data-selected={selectedSet.has(keys[index]) ? "true" : undefined}
                                    className={cn("border-b align-middle last:border-0",
                                        selection.current === keys[index] ? "bg-accent" : selectedSet.has(keys[index]) && "bg-accent/50")}>
                                    <td className="cursor-pointer select-none px-2 py-1 tabular-nums text-muted-foreground"
                                        onMouseDown={onCellMouseDown} onClick={(event) => onCellClick(keys[index], null, event)}>{lineNo}</td>
                                    {columns.map((column) => {
                                        const cellEditable = isEditableColumn(column, writable, readOnly)
                                        const isCurrentCell = selection.current === keys[index] && currentColumn === column.name
                                        const select = {
                                            onMouseDownCapture: onCellMouseDown,
                                            onClickCapture: (event: React.MouseEvent) => onCellClick(keys[index], column.name, event),
                                            onFocusCapture: () => { setCurrentColumn(column.name); if (selection.current !== keys[index]) setSelection(clickRow(selection, keys, keys[index], { shift: false, toggle: false })) },
                                        }
                                        const withTotals = (cell: React.ReactElement) => isCurrentCell && summary && dismissedSummary !== summaryKey ? (
                                            <SelectionSummaryPopover key={column.name} summary={summary} label={column.label || column.name} onDismiss={() => setDismissedSummary(summaryKey)}>
                                                {cell}
                                            </SelectionSummaryPopover>
                                        ) : cell
                                        if (!cellEditable) {
                                            return withTotals(<td key={column.name} className={cn("max-w-48 truncate px-2 py-1", isCurrentCell && "shadow-[inset_0_0_0_1px_hsl(var(--ring))]")} {...select}>{cellText(row, column)}</td>)
                                        }
                                        const endpoint = column.type === "reference" && column.referenceType ? resolveRefEndpoint(column.referenceType) : null
                                        return (
                                            withTotals(<td key={column.name} className={cn("px-1 py-1", isCurrentCell && "shadow-[inset_0_0_0_1px_hsl(var(--ring))]")} data-field={`${part.name}.${index}.${column.name}`} {...select}>
                                                {endpoint ? (
                                                    <ReferenceField
                                                        value={typeof row[column.name] === "string" ? String(row[column.name]) : ""}
                                                        displayName={referenceDisplayName(row, column.name)}
                                                        onChange={(value, display) => onChange(setCell(rows, index, column.name, value, display))}
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
                                    {editable && (
                                        <td className="whitespace-nowrap px-1 py-1 text-right">
                                            <Button type="button" variant="ghost" size="icon" className="h-7 w-7" disabled={disabled || index === 0}
                                                aria-label={`Переместить строку ${lineNo} вверх`} onClick={() => onChange(moveRow(rows, index, -1))}>
                                                <ArrowUp className="h-3.5 w-3.5" />
                                            </Button>
                                            <Button type="button" variant="ghost" size="icon" className="h-7 w-7" disabled={disabled || index === rows.length - 1}
                                                aria-label={`Переместить строку ${lineNo} вниз`} onClick={() => onChange(moveRow(rows, index, 1))}>
                                                <ArrowDown className="h-3.5 w-3.5" />
                                            </Button>
                                            <Button type="button" variant="ghost" size="icon" className="h-7 w-7" disabled={disabled}
                                                aria-label={`Удалить строку ${lineNo}`} onClick={() => onChange(removeRow(rows, index))}>
                                                <Trash2 className="h-3.5 w-3.5" />
                                            </Button>
                                        </td>
                                    )}
                                </tr>
                            )
                        })}
                    </tbody>
                </table>
            </div>
        </section>
    )
}

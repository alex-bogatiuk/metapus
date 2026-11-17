"use client"

import React from "react"
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { ReferenceField } from "@/components/shared/reference-field"
import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { resolveRefEndpoint } from "@/lib/reference-endpoint"
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
export function TablePartEditor({ part, rows, writable, readOnly, disabled, hiddenColumns, onChange, renderInput }: TablePartEditorProps) {
    const columns = part.columns.filter((column) => !hiddenColumns.has(column.name) && !LINE_IDENTITY_COLUMNS.has(column.name))
    const editable = !readOnly && writable !== null
    const title = part.label || part.name

    return (
        <section className="mt-8 space-y-2" aria-label={title} data-table-part={part.name}>
            <div className="flex items-center justify-between gap-2">
                <Label className="text-sm font-semibold">{title}</Label>
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
                                <tr key={rowKey(row, index)} className="border-b align-middle last:border-0">
                                    <td className="px-2 py-1 tabular-nums text-muted-foreground">{lineNo}</td>
                                    {columns.map((column) => {
                                        const cellEditable = isEditableColumn(column, writable, readOnly)
                                        if (!cellEditable) {
                                            return <td key={column.name} className="max-w-48 truncate px-2 py-1">{cellText(row, column)}</td>
                                        }
                                        const endpoint = column.type === "reference" && column.referenceType ? resolveRefEndpoint(column.referenceType) : null
                                        return (
                                            <td key={column.name} className="px-1 py-1" data-field={`${part.name}.${index}.${column.name}`}>
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
                                            </td>
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

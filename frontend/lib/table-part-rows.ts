import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"

/** Line identity columns the server owns; № is rendered from the position. */
export const LINE_IDENTITY_COLUMNS = new Set(["lineId", "lineNo"])

/**
 * The response key holding a reference column's display ({id, name, code}):
 * the json name without its "Id" suffix, e.g. productId → product.
 */
export function referenceDisplayKey(column: string): string | null {
    return column.endsWith("Id") && column.length > 2 ? column.slice(0, -2) : null
}

/** The display name the server resolved for a reference cell, if any. */
export function referenceDisplayName(row: JsonObject, column: string): string | undefined {
    const key = referenceDisplayKey(column)
    const display = key ? row[key] : undefined
    if (display && typeof display === "object" && !Array.isArray(display) && typeof display.name === "string") {
        return display.name
    }
    return undefined
}

/**
 * A column is editable when the form is, the line request accepts it and the
 * model does not mark it read-only.
 */
export function isEditableColumn(column: MetadataField, writable: readonly string[] | null, readOnly: boolean): boolean {
    return !readOnly && !!writable?.includes(column.name) && !column.readOnly && !LINE_IDENTITY_COLUMNS.has(column.name)
}

/** Sets a cell; a reference keeps the picked display so the row shows it before saving. */
export function setCell(rows: JsonObject[], index: number, column: string, value: JsonValue, display?: string): JsonObject[] {
    return rows.map((row, i) => {
        if (i !== index) return row
        const next: JsonObject = { ...row, [column]: value }
        const key = referenceDisplayKey(column)
        if (key && display !== undefined) {
            next[key] = typeof value === "string" && value ? { id: value, name: display } : null
        }
        return next
    })
}

/**
 * Client-only identity of a line not saved yet, for stable React keys; the
 * request projection drops it with every other field the line contract lacks.
 */
export const ROW_KEY = "__rowKey"

let nextRowKey = 0

export function addRow(rows: JsonObject[]): JsonObject[] {
    nextRowKey += 1
    return [...rows, { [ROW_KEY]: `new-${nextRowKey}` }]
}

/** A stable key for a row: its lineId once saved, its client key before. */
export function rowKey(row: JsonObject, index: number): string {
    if (typeof row.lineId === "string" && row.lineId) return row.lineId
    if (typeof row[ROW_KEY] === "string") return String(row[ROW_KEY])
    return `row-${index}`
}

export function removeRow(rows: JsonObject[], index: number): JsonObject[] {
    return rows.filter((_, i) => i !== index)
}

/** Moves a row up (-1) or down (+1); lines are renumbered by position on save. */
export function moveRow(rows: JsonObject[], index: number, delta: -1 | 1): JsonObject[] {
    const target = index + delta
    if (target < 0 || target >= rows.length) return rows
    const next = [...rows]
    ;[next[index], next[target]] = [next[target], next[index]]
    return next
}

/** A line the form can tell is incomplete before sending it. */
export class LineInputError extends Error {}

/**
 * Checks the required, editable cells of every writable part and throws the
 * first empty one as "<part>, строка N: заполните «<column>»".
 */
export function checkRequiredCells(
    parts: readonly { name: string; label?: string; columns: MetadataField[] }[],
    draft: JsonObject,
    writableByPart: Readonly<Record<string, readonly string[]>>,
): void {
    for (const part of parts) {
        const writable = writableByPart[part.name]
        const rows = draft[part.name]
        if (!writable || !Array.isArray(rows)) continue
        rows.forEach((row, index) => {
            if (row === null || typeof row !== "object" || Array.isArray(row)) return
            for (const column of part.columns) {
                if (!column.required || !isEditableColumn(column, writable, false)) continue
                const value = row[column.name]
                if (value === undefined || value === null || value === "") {
                    throw new LineInputError(`${part.label || part.name}, строка ${index + 1}: заполните «${column.label || column.name}»`)
                }
            }
        })
    }
}

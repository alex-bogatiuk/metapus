import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { evaluateExpression } from "@/lib/calc-engine"
import { MoneyDecimal } from "@/lib/money"
import { rowKey, setCell } from "@/lib/table-part-rows"

/**
 * Spreadsheet-style input for table parts: lines pasted from Excel or Google
 * Sheets, cell text converted by column type, and the calculator of number
 * cells. Pure functions; the editor resolves references before pasting.
 */

/**
 * Splits clipboard text into rows of cells: tabs between cells, newlines
 * between rows, and Excel's quoting ("a ""b""", a cell with a newline in
 * quotes). The trailing newline spreadsheets add is dropped.
 */
export function parseClipboardGrid(text: string): string[][] {
    const rows: string[][] = []
    let row: string[] = []
    let cell = ""
    let quoted = false
    let atCellStart = true
    for (let i = 0; i < text.length; i++) {
        const char = text[i]
        if (quoted) {
            if (char === '"' && text[i + 1] === '"') { cell += '"'; i++ }
            else if (char === '"') quoted = false
            else cell += char
            continue
        }
        if (char === '"' && atCellStart) { quoted = true; atCellStart = false; continue }
        if (char === "\t") { row.push(cell); cell = ""; atCellStart = true; continue }
        if (char === "\n" || char === "\r") {
            if (char === "\r" && text[i + 1] === "\n") i++
            row.push(cell)
            rows.push(row)
            row = []
            cell = ""
            atCellStart = true
            continue
        }
        cell += char
        atCellStart = false
    }
    if (cell !== "" || row.length) {
        row.push(cell)
        rows.push(row)
    }
    return rows
}

/** "1 234,5" or "1234.5" as a number; null when it is not one. */
export function parseNumberText(text: string): number | null {
    const normalized = text.replace(/[\s  ]/g, "").replace(",", ".")
    if (!/^-?\d+(\.\d+)?$/.test(normalized)) return null
    return Number(normalized)
}

const TRUE_WORDS = new Set(["да", "yes", "true", "1", "+", "✓", "истина"])
const FALSE_WORDS = new Set(["нет", "no", "false", "0", "-", "", "ложь"])

export function parseBooleanText(text: string): boolean | null {
    const word = text.trim().toLowerCase()
    if (TRUE_WORDS.has(word)) return true
    if (FALSE_WORDS.has(word)) return false
    return null
}

/** "31.12.2026" or "2026-12-31" as an ISO date; null otherwise. */
export function parseDateText(text: string): string | null {
    const value = text.trim()
    const russian = /^(\d{1,2})\.(\d{1,2})\.(\d{4})$/.exec(value)
    const iso = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
    const parts = russian ? [russian[3], russian[2], russian[1]] : iso ? [iso[1], iso[2], iso[3]] : null
    if (!parts) return null
    const [year, month, day] = parts
    const date = new Date(Date.UTC(Number(year), Number(month) - 1, Number(day)))
    if (date.getUTCMonth() !== Number(month) - 1 || date.getUTCDate() !== Number(day)) return null
    return `${year}-${month.padStart(2, "0")}-${day.padStart(2, "0")}`
}

/**
 * What a number cell takes from what was typed (1C and Excel style):
 * a number ("1 234,5"), an expression ("500*4", "1,5*(2+3)"), or a change of
 * the value the cell had ("+5", "*2", "/4"). Whole for integer columns
 * (rounded), else trimmed to ten places. Null when it is none of these.
 */
export function evaluateNumberInput(text: string, previous: number | null, integer: boolean): number | null {
    const compact = text.replace(/[\s  ]/g, "").replace(/,/g, ".")
    if (!compact) return null
    const plain = parseNumberText(compact)
    let value: number | null = plain
    if (value === null) {
        const relative = /^[+*/]/.test(compact) && previous !== null ? `${previous}${compact}` : compact
        value = evaluateExpression(relative)?.value ?? null
    }
    if (value === null || !Number.isFinite(value)) return null
    return integer ? Math.round(value) : new MoneyDecimal(value).toDecimalPlaces(10).toNumber()
}

/** A converted cell, or null when the text names nothing the column takes. */
export type CellConversion = { value: JsonValue; display?: string } | null

/** Converts pasted text by the column's type; references go through `resolve`. */
export function convertCellText(column: MetadataField, text: string, resolve: (column: string, text: string) => { id: string; name: string } | null): CellConversion {
    const value = text.trim()
    switch (column.type) {
        case "reference": {
            const found = resolve(column.name, value)
            return found ? { value: found.id, display: found.name } : null
        }
        case "integer":
        case "number":
        case "decimal": {
            const number = parseNumberText(value)
            if (number === null || (column.type === "integer" && !Number.isInteger(number))) return null
            return { value: number }
        }
        case "boolean": {
            const flag = parseBooleanText(value)
            return flag === null ? null : { value: flag }
        }
        case "date": {
            const date = parseDateText(value)
            return date ? { value: date } : null
        }
        default: {
            const option = column.enumValues?.find((item) => item.label.toLowerCase() === value.toLowerCase() || item.value === value)
            if (column.enumValues?.length) return option ? { value: option.value } : null
            return { value }
        }
    }
}

export interface PasteFailure {
    line: number
    column: string
    text: string
}

/**
 * Lays a pasted grid over the lines from a cell, as a spreadsheet does: the
 * first pasted cell lands on (line, column), lines past the end are added.
 * Only columns the user may edit take values; empty pasted cells change
 * nothing; a cell whose text converts to nothing is left as it was and
 * reported. Returns the lines, the keys of the lines it wrote to, and failures.
 */
export function pasteGrid(
    rows: readonly JsonObject[],
    start: { line: number; column: number },
    grid: readonly (readonly string[])[],
    columns: readonly MetadataField[],
    editable: (column: MetadataField) => boolean,
    convert: (column: MetadataField, text: string) => CellConversion,
    newRow: () => JsonObject,
): { rows: JsonObject[]; touched: string[]; failures: PasteFailure[] } {
    let next = [...rows]
    const touched = new Set<string>()
    const failures: PasteFailure[] = []
    grid.forEach((cells, i) => {
        const line = start.line + i
        if (cells.every((cell) => cell.trim() === "")) return
        if (line >= next.length) next = [...next, newRow()]
        cells.forEach((text, j) => {
            const column = columns[start.column + j]
            if (!column || !editable(column) || text.trim() === "") return
            const converted = convert(column, text)
            if (!converted) {
                failures.push({ line, column: column.label || column.name, text: text.trim() })
                return
            }
            next = setCell(next, line, column.name, converted.value, converted.display)
            touched.add(rowKey(next[line], line))
        })
    })
    return { rows: next, touched: [...touched], failures }
}

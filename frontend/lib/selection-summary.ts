import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { PairableField } from "@/lib/form-layout"
import { MoneyDecimal, minorUnitsSchema, rawMinorToMajor } from "@/lib/money"

/**
 * Totals of a numeric column over selected rows (1C/Excel style): sum,
 * average and count, computed exactly in decimal. Values of different units
 * (currencies, tokens) are never added together: each unit gets its own group.
 */

/** One cell's number as a canonical decimal string, with its unit ("" if none). */
export interface SummaryOperand {
    value: string
    unit: string
}

export interface SummaryGroup {
    unit: string
    count: number
    sum: string
    average: string
}

export interface SelectionSummary {
    /** Selected rows the summary covers, numeric or not. */
    rows: number
    groups: SummaryGroup[]
}

/** Extracts a row's operand for a column; null when the cell holds no number. */
export type SummaryExtractor = (row: JsonObject) => SummaryOperand | null

const DECIMAL_TEXT = /^-?\d+(?:\.\d+)?$/

function scaleOf(value: string): number {
    const dot = value.indexOf(".")
    return dot === -1 ? 0 : value.length - dot - 1
}

/** A plain number or decimal text as an operand; anything else is not a number. */
export function numericOperand(value: JsonValue | undefined, unit = ""): SummaryOperand | null {
    if (typeof value === "number") return Number.isFinite(value) ? { value: new MoneyDecimal(value).toFixed(), unit } : null
    if (typeof value === "string" && DECIMAL_TEXT.test(value)) return { value, unit }
    return null
}

export function summarize(operands: readonly (SummaryOperand | null | undefined)[]): SelectionSummary | null {
    const byUnit = new Map<string, { sum: InstanceType<typeof MoneyDecimal>; count: number; scale: number }>()
    for (const operand of operands) {
        if (!operand) continue
        const group = byUnit.get(operand.unit) ?? { sum: new MoneyDecimal(0), count: 0, scale: 0 }
        group.sum = group.sum.plus(operand.value)
        group.count += 1
        group.scale = Math.max(group.scale, scaleOf(operand.value))
        byUnit.set(operand.unit, group)
    }
    if (byUnit.size === 0) return null
    const groups = [...byUnit.entries()].map(([unit, { sum, count, scale }]) => {
        // The average keeps the values' scale and up to two more places, without trailing zeros.
        const average = sum.div(count).toDecimalPlaces(scale + 2)
        const trimmed = average.toFixed(Math.max(scale, average.decimalPlaces()))
        return { unit, count, sum: sum.toFixed(scale), average: trimmed }
    })
    return { rows: operands.length, groups }
}

/** "1234567.5" → "1 234 567,5" (non-breaking spaces, decimal comma). */
export function formatSummaryNumber(value: string): string {
    const negative = value.startsWith("-")
    const [integer, fraction] = (negative ? value.slice(1) : value).split(".")
    const grouped = integer.replace(/\B(?=(\d{3})+(?!\d))/g, " ")
    return `${negative ? "-" : ""}${grouped}${fraction ? `,${fraction}` : ""}`
}

/** What a total puts on the clipboard: digits and a decimal comma, ready for a calculator. */
export function summaryCopyText(value: string): string {
    return value.replace(".", ",")
}

/** The label a reference unit shows: its code, else its name, else the raw id. */
function unitLabel(row: JsonObject, unit: PairableField): string {
    const display = unit.name.endsWith("Id") ? row[unit.name.slice(0, -2)] : undefined
    if (display && typeof display === "object" && !Array.isArray(display)) {
        if (typeof display.code === "string" && display.code) return display.code
        if (typeof display.name === "string" && display.name) return display.name
    }
    const raw = row[unit.name]
    return typeof raw === "string" ? raw : ""
}

const NUMERIC_TYPES = new Set(["integer", "number", "decimal"])

/** Per-unit values (a price, a rate, a percent) that mean nothing when added up. */
const NON_ADDITIVE = /(price|rate|percent|pct|ratio|coef|factor|multiplier)/i

/**
 * Whether a column's values add up to a meaningful total (1C "Подвал"): a
 * number or money, not a per-unit value such as a price or a rate.
 */
export function isSummableField(field: PairableField): boolean {
    return (field.type === "money" || NUMERIC_TYPES.has(field.type)) && !NON_ADDITIVE.test(field.name)
}

/** A total group as shown: "1 234,5" or "1 234,5 USDT". */
export function summaryGroupText(group: SummaryGroup): string {
    return group.unit ? `${formatSummaryNumber(group.sum)} ${group.unit}` : formatSummaryNumber(group.sum)
}

/**
 * The extractor of a metadata field, or undefined when the field is not a
 * number. Money is minor units in the row's currency; another number takes
 * its unit from the paired currency or token field, when there is one.
 */
export function fieldSummaryExtractor(field: PairableField, key: string, unit?: PairableField): SummaryExtractor | undefined {
    if (field.type === "money") {
        return (row) => {
            const minor = minorUnitsSchema.safeParse(row[key])
            const currency = row.currency
            if (!minor.success || !currency || typeof currency !== "object" || Array.isArray(currency)) return null
            const scale = currency.decimalPlaces
            if (typeof scale !== "number") return null
            return { value: rawMinorToMajor(minor.data, scale), unit: typeof currency.symbol === "string" ? currency.symbol : "" }
        }
    }
    if (!NUMERIC_TYPES.has(field.type)) return undefined
    return (row) => numericOperand(row[key], unit ? unitLabel(row, unit) : "")
}

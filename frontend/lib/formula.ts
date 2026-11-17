import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { MoneyDecimal } from "@/lib/money"

/**
 * Calculated fields (meta:"calc:quantity*price") computed as the user types,
 * by the same rules as the server (internal/core/formula and v1 auto_calc):
 * numbers, sibling field names, + - * /, parentheses and unary minus in exact
 * decimal arithmetic; a quotient rounded to 16 places, division by zero = 0;
 * the result rounded half away from zero to the field's scale. A header can
 * total a table-part column, sum(items.amount). The server computes the
 * stored value; this one only keeps the form in step.
 */

type Decimal = InstanceType<typeof MoneyDecimal>

/** Places a quotient is rounded to (formula.DivisionPlaces on the server). */
const DIVISION_PLACES = 16

type FormulaNode =
    | { kind: "number"; value: Decimal }
    | { kind: "ref"; name: string }
    | { kind: "sum"; part: string; field: string }
    | { kind: "negate"; operand: FormulaNode }
    | { kind: "binary"; op: "+" | "-" | "*" | "/"; left: FormulaNode; right: FormulaNode }

/** A table-part column a formula totals: sum(part.field). */
export interface SumRef {
    part: string
    field: string
}

/** A formula's values: the row's own fields and the totals of its table parts' columns. */
export interface FormulaEnv {
    value: (name: string) => Decimal
    sum: (ref: SumRef) => Decimal
}

export interface Formula {
    source: string
    /** Names the formula reads, in order of first use. */
    vars: string[]
    /** Table-part columns the formula totals. */
    sums: SumRef[]
    evaluate: (env: FormulaEnv) => Decimal
}

const cache = new Map<string, Formula | null>()

/** Parses a formula; null when it does not parse (the server rejects it at registration). */
export function parseFormula(source: string): Formula | null {
    if (cache.has(source)) return cache.get(source) ?? null
    let pos = 0
    const vars: string[] = []
    const sums: SumRef[] = []
    const name = (): string => {
        skip()
        const start = pos
        if (/[\p{L}_]/u.test(source[pos] ?? "")) while (/[\p{L}\p{N}_]/u.test(source[pos] ?? "")) pos++
        return source.slice(start, pos)
    }
    const skip = () => { while (source[pos] === " " || source[pos] === "\t") pos++ }
    const peek = () => { skip(); return source[pos] ?? "" }
    const fail = (): never => { throw new Error(`formula ${source}`) }
    const sum = (): FormulaNode => {
        let left = product()
        for (let op = peek(); op === "+" || op === "-"; op = peek()) {
            pos++
            left = { kind: "binary", op, left, right: product() }
        }
        return left
    }
    const product = (): FormulaNode => {
        let left = unary()
        for (let op = peek(); op === "*" || op === "/"; op = peek()) {
            pos++
            left = { kind: "binary", op, left, right: unary() }
        }
        return left
    }
    const unary = (): FormulaNode => {
        if (peek() === "-") {
            pos++
            return { kind: "negate", operand: unary() }
        }
        return primary()
    }
    const primary = (): FormulaNode => {
        const c = peek()
        if (c === "(") {
            pos++
            const inner = sum()
            if (peek() !== ")") fail()
            pos++
            return inner
        }
        if (/[0-9]/.test(c)) {
            const start = pos
            while (/[0-9.]/.test(source[pos] ?? "")) pos++
            const text = source.slice(start, pos)
            if (!/^\d+(\.\d+)?$/.test(text)) fail()
            return { kind: "number", value: new MoneyDecimal(text) }
        }
        if (/[\p{L}_]/u.test(c)) {
            const word = name()
            if (peek() === "(") {
                // sum(part.field), the only function: the total of a table-part column.
                if (word !== "sum") fail()
                pos++
                const part = name()
                if (!part || peek() !== ".") fail()
                pos++
                const field = name()
                if (!field || peek() !== ")") fail()
                pos++
                if (!sums.some((ref) => ref.part === part && ref.field === field)) sums.push({ part, field })
                return { kind: "sum", part, field }
            }
            if (!vars.includes(word)) vars.push(word)
            return { kind: "ref", name: word }
        }
        return fail()
    }
    let formula: Formula | null = null
    try {
        if (peek() === "") fail()
        const root = sum()
        if (peek() !== "") fail()
        const run = (node: FormulaNode, env: FormulaEnv): Decimal => {
            switch (node.kind) {
                case "number": return node.value
                case "ref": return env.value(node.name)
                case "sum": return env.sum({ part: node.part, field: node.field })
                case "negate": return run(node.operand, env).neg()
                case "binary": {
                    const left = run(node.left, env)
                    const right = run(node.right, env)
                    if (node.op === "+") return left.plus(right)
                    if (node.op === "-") return left.minus(right)
                    if (node.op === "*") return left.times(right)
                    return right.isZero() ? new MoneyDecimal(0) : left.div(right).toDecimalPlaces(DIVISION_PLACES, MoneyDecimal.ROUND_HALF_UP)
                }
            }
        }
        formula = { source, vars, sums, evaluate: (env) => run(root, env) }
    } catch {
        formula = null
    }
    cache.set(source, formula)
    return formula
}

/**
 * The places a field's result is rounded to, as the server rounds by the Go
 * type: money (minor units) and integers whole, Quantity (valueScale 10000)
 * and decimals four places.
 */
export function formulaScale(field: MetadataField): number {
    if (field.type === "money" || field.type === "integer") return 0
    if (field.valueScale && field.valueScale > 1) return Math.round(Math.log10(field.valueScale))
    return field.scale ?? 4
}

function operand(value: JsonValue | undefined): Decimal {
    if (typeof value === "number" && Number.isFinite(value)) return new MoneyDecimal(value)
    if (typeof value === "string" && /^-?\d+(\.\d+)?$/.test(value.trim())) return new MoneyDecimal(value.trim())
    return new MoneyDecimal(0)
}

/** The calculated fields in the order they compute: one read by another goes first. */
function ordered(fields: readonly MetadataField[]): MetadataField[] {
    const calculated = new Map(fields.filter((field) => field.calc).map((field) => [field.name, field]))
    const out: MetadataField[] = []
    const seen = new Set<string>()
    const visit = (field: MetadataField, path: Set<string>) => {
        if (seen.has(field.name) || path.has(field.name)) return
        path.add(field.name)
        for (const name of parseFormula(field.calc ?? "")?.vars ?? []) {
            const dependency = calculated.get(name)
            if (dependency) visit(dependency, path)
        }
        seen.add(field.name)
        out.push(field)
    }
    calculated.forEach((field) => visit(field, new Set()))
    return out
}

/** Whether any of the fields is calculated. */
export function hasFormulas(fields: readonly MetadataField[]): boolean {
    return fields.some((field) => field.calc)
}

/**
 * Recomputes a row's calculated fields. Keeps each value's JSON type (a
 * decimal the server sends as text stays text) and returns the same object
 * when nothing changed.
 */
export function applyFormulas(row: JsonObject, fields: readonly MetadataField[]): JsonObject {
    let next = row
    for (const field of ordered(fields)) {
        const formula = parseFormula(field.calc ?? "")
        if (!formula) continue
        const row = next
        const result = formula.evaluate({
            value: (name) => operand(row[name]),
            // A header totals its table part's lines, already recomputed by the part.
            sum: ({ part, field: column }) => {
                const lines = row[part]
                if (!Array.isArray(lines)) return new MoneyDecimal(0)
                return lines.reduce<Decimal>((total, line) =>
                    line && typeof line === "object" && !Array.isArray(line) ? total.plus(operand(line[column])) : total, new MoneyDecimal(0))
            },
        }).toDecimalPlaces(formulaScale(field), MoneyDecimal.ROUND_HALF_UP)
        const current = next[field.name]
        // The wire's JSON type: money (minor units) and decimals travel as text, other numbers as numbers.
        const text = typeof current === "string" || ((current === undefined || current === null) && (field.type === "decimal" || field.type === "money"))
        const value: JsonValue = text ? result.toFixed() : result.toNumber()
        if (current === value || (current !== undefined && current !== null && operand(current).equals(result) && typeof current === typeof value)) continue
        next = { ...next, [field.name]: value }
    }
    return next
}

/** A formula for people: names replaced by the fields' labels ("Количество × Цена"). */
export function formulaLabel(source: string, fields: readonly MetadataField[], parts: readonly { name: string; label?: string; columns: readonly MetadataField[] }[] = []): string {
    const labels = new Map(fields.map((field) => [field.name, field.label || field.name]))
    const id = "[\\p{L}_][\\p{L}\\p{N}_]*"
    // One pass: sum(items.amount) → Σ «Товары: Сумма», a field name → its label.
    return source
        .replace(new RegExp(`sum\\s*\\(\\s*(${id})\\s*\\.\\s*(${id})\\s*\\)|(${id})`, "gu"), (match, partName?: string, column?: string, name?: string) => {
            if (!partName || !column) return labels.get(name ?? match) ?? match
            const part = parts.find((p) => p.name === partName)
            return `Σ «${part?.label || partName}: ${part?.columns.find((c) => c.name === column)?.label || column}»`
        })
        .replace(/\s*\*\s*/g, " × ").replace(/\s*\/\s*/g, " ÷ ").replace(/\s*([+-])\s*/g, " $1 ")
}

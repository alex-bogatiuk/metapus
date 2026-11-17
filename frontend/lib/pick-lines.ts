import type { JsonObject, JsonValue } from "@/lib/json-value"
import type { MetadataField } from "@/lib/schemas/metadata"
import { MoneyDecimal } from "@/lib/money"
import { ROW_KEY, isEditableColumn, referenceDisplayKey, rowKey } from "@/lib/table-part-rows"

/**
 * "Подбор" (1C): picking catalog items into a table part through a cart.
 * The part says what it picks by its metadata: the item is its editable
 * reference column, the quantity its editable quantity-like number column.
 */

export interface PickTarget {
    /** The reference column a pick fills (e.g. productId). */
    item: MetadataField
    /** The number column a pick's quantity goes to; null when the part has none. */
    quantity: MetadataField | null
}

const QUANTITY_NAME = /^(quantity|qty|count)$|(Quantity|Qty)$/
const QUANTITY_TYPES = new Set(["integer", "number", "decimal"])

/** What a part picks into, or null when it has no editable reference column to pick. */
export function pickTarget(columns: readonly MetadataField[], writable: readonly string[] | null): PickTarget | null {
    const editable = columns.filter((column) => isEditableColumn(column, writable, false))
    const references = editable.filter((column) => column.type === "reference" && !!column.referenceType)
    const item = references.find((column) => column.required) ?? references[0]
    if (!item) return null
    const quantity = editable.find((column) => QUANTITY_TYPES.has(column.type) && QUANTITY_NAME.test(column.name)) ?? null
    return { item, quantity }
}

/** A catalog item in the cart; quantity is a canonical decimal string. */
export interface CartLine {
    id: string
    name: string
    code?: string
    quantity: string
}

/**
 * A typed quantity as a canonical decimal ("2,5" → "2.5"), or null when it
 * is not a positive number (or not whole for an integer column).
 */
export function parseQuantity(text: string, integer: boolean): string | null {
    const normalized = text.trim().replace(",", ".")
    if (!/^\d+(\.\d+)?$/.test(normalized)) return null
    const value = new MoneyDecimal(normalized)
    if (value.lte(0) || (integer && !value.isInteger())) return null
    return value.toFixed()
}

/**
 * Splits a search into a quantity and the text: "3*гвозди" or "3 * гвозди"
 * picks 3 of what "гвозди" finds. Plain text has no quantity.
 */
export function splitQuantityPrefix(search: string, integer: boolean): { quantity: string | null; text: string } {
    const match = /^\s*(\d+(?:[.,]\d+)?)\s*\*\s*(.*)$/.exec(search)
    if (!match) return { quantity: null, text: search }
    return { quantity: parseQuantity(match[1], integer), text: match[2] }
}

/** Adds a quantity of an item: an item already in the cart grows, a new one goes last. */
export function addToCart(cart: readonly CartLine[], item: Omit<CartLine, "quantity">, quantity: string): CartLine[] {
    const existing = cart.find((line) => line.id === item.id)
    if (!existing) return [...cart, { ...item, quantity }]
    const total = new MoneyDecimal(existing.quantity).plus(quantity).toFixed()
    return cart.map((line) => (line.id === item.id ? { ...line, quantity: total } : line))
}

/** Sets an item's quantity; zero or less takes it out of the cart. */
export function setCartQuantity(cart: readonly CartLine[], id: string, quantity: string): CartLine[] {
    if (new MoneyDecimal(quantity).lte(0)) return cart.filter((line) => line.id !== id)
    return cart.map((line) => (line.id === id ? { ...line, quantity } : line))
}

/** The total quantity of the cart. */
export function cartTotal(cart: readonly CartLine[]): string {
    return cart.reduce((total, line) => total.plus(line.quantity), new MoneyDecimal(0)).toFixed()
}

/** How much of each item the document already holds (by quantity, else by lines). */
export function documentQuantities(rows: readonly JsonObject[], target: PickTarget): Map<string, string> {
    const totals = new Map<string, InstanceType<typeof MoneyDecimal>>()
    for (const row of rows) {
        const id = row[target.item.name]
        if (typeof id !== "string" || !id) continue
        const raw = target.quantity ? row[target.quantity.name] : 1
        const amount = typeof raw === "number" || (typeof raw === "string" && raw !== "") ? new MoneyDecimal(raw) : new MoneyDecimal(target.quantity ? 0 : 1)
        totals.set(id, (totals.get(id) ?? new MoneyDecimal(0)).plus(amount))
    }
    return new Map([...totals].map(([id, total]) => [id, total.toFixed()]))
}

/** Adds a decimal to a cell, keeping the cell's JSON type (a string stays a string). */
function plusCell(value: JsonValue | undefined, quantity: string): JsonValue {
    const base = typeof value === "number" || (typeof value === "string" && value !== "") ? value : 0
    const total = new MoneyDecimal(base).plus(quantity).toFixed()
    return typeof value === "string" ? total : Number(total)
}

let nextPickKey = 0

/**
 * Puts the cart into the part. With merge, an item the document already has
 * adds its quantity to the first such line (when the part has a quantity);
 * otherwise each cart line becomes a new line at the end. Returns the rows
 * and the keys of the lines added or changed.
 */
export function transferPicks(rows: readonly JsonObject[], cart: readonly CartLine[], target: PickTarget, merge: boolean): { rows: JsonObject[]; touched: string[]; added: number; merged: number } {
    const next = [...rows]
    const touched: string[] = []
    let added = 0
    let merged = 0
    const display = referenceDisplayKey(target.item.name)
    for (const line of cart) {
        const index = merge && target.quantity ? next.findIndex((row) => row[target.item.name] === line.id) : -1
        if (index !== -1 && target.quantity) {
            next[index] = { ...next[index], [target.quantity.name]: plusCell(next[index][target.quantity.name], line.quantity) }
            touched.push(rowKey(next[index], index))
            merged += 1
            continue
        }
        nextPickKey += 1
        const row: JsonObject = { [ROW_KEY]: `pick-${nextPickKey}`, [target.item.name]: line.id }
        if (display) row[display] = line.code ? { id: line.id, name: line.name, code: line.code } : { id: line.id, name: line.name }
        if (target.quantity) row[target.quantity.name] = Number(line.quantity)
        next.push(row)
        touched.push(rowKey(row, next.length - 1))
        added += 1
    }
    return { rows: next, touched, added, merged }
}

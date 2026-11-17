import { test, expect } from "@playwright/test"
import type { JsonObject } from "../../lib/json-value"
import type { MetadataField } from "../../lib/schemas/metadata"
import { findCells, missingCells, sortRows } from "../../lib/table-part-rows"
import { createScanDetector } from "../../lib/barcode-scan"

const columns = [
    { name: "productId", label: "Товар", type: "reference", required: true },
    { name: "quantity", label: "Количество", type: "integer", required: true },
    { name: "note", label: "Примечание", type: "string" },
    { name: "batch", label: "Партия", type: "string", required: true, readOnly: true },
] as MetadataField[]
const rows: JsonObject[] = [
    { lineId: "a", productId: "p1", product: { id: "p1", name: "Шурупы" }, quantity: 5, note: "" },
    { lineId: "b", productId: "", quantity: 0, note: "срочно" },
    { lineId: "c", productId: "p2", product: { id: "p2", name: "Гвозди" }, quantity: null, note: "Срочно, второй склад" },
]
const textOf = (row: JsonObject, column: MetadataField) => {
    if (column.type === "reference") {
        const display = row[column.name.slice(0, -2)]
        return display && typeof display === "object" && !Array.isArray(display) && typeof display.name === "string" ? display.name : ""
    }
    const value = row[column.name]
    return value === undefined || value === null ? "" : String(value)
}

test("required cells the user can fill and left empty are found in reading order", () => {
    const editable = (column: MetadataField) => !column.readOnly
    expect(missingCells(rows, columns, editable)).toEqual([
        { key: "b", line: 1, column: "productId" },
        { key: "c", line: 2, column: "quantity" },
    ])
})

test("search finds cells by their shown text in any case", () => {
    expect(findCells(rows, columns, "СРОЧНО", textOf).map((cell) => `${cell.key}.${cell.column}`)).toEqual(["b.note", "c.note"])
    expect(findCells(rows, columns, "гвоз", textOf).map((cell) => `${cell.key}.${cell.column}`)).toEqual(["c.productId"])
    expect(findCells(rows, columns, "  ", textOf)).toEqual([])
})

test("ordering by a column changes the lines' order, empty values last, ties stable", () => {
    const keys = (list: JsonObject[]) => list.map((row) => row.lineId)
    expect(keys(sortRows(rows, columns[1], "asc", textOf))).toEqual(["b", "a", "c"])
    expect(keys(sortRows(rows, columns[1], "desc", textOf))).toEqual(["a", "b", "c"])
    expect(keys(sortRows(rows, columns[0], "asc", textOf))).toEqual(["c", "a", "b"])
    const money = { name: "price", type: "money" } as MetadataField
    expect(keys(sortRows([{ lineId: "x", price: "1000" }, { lineId: "y", price: "200" }], money, "asc", textOf))).toEqual(["y", "x"])
})

test("a scanner's burst ended by Enter is a scan; a person's typing is not", () => {
    const scan = createScanDetector()
    const feed = (keys: string, start: number, gap: number) => [...keys].map((key, i) => scan.push(key, start + i * gap))
    expect(feed("4600123", 0, 8)).toEqual(Array(7).fill(null))
    expect(scan.push("Enter", 56)).toBe("4600123")
    // A person: 120 ms between keys.
    feed("4600123", 1000, 120)
    expect(scan.push("Enter", 1000 + 7 * 120)).toBeNull()
    // Too short a burst, or a pause before Enter.
    feed("P1", 5000, 5)
    expect(scan.push("Enter", 5010)).toBeNull()
    feed("P-001", 6000, 5)
    expect(scan.push("Enter", 6500)).toBeNull()
    // A slow key starts a new burst: only the quick tail counts.
    scan.push("x", 7000)
    feed("P-002", 7300, 5)
    expect(scan.pending()).toBe("P-002")
    expect(scan.push("Enter", 7325)).toBe("P-002")
})

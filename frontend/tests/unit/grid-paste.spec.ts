import { test, expect } from "@playwright/test"
import type { JsonObject } from "../../lib/json-value"
import type { MetadataField } from "../../lib/schemas/metadata"
import {
    convertCellText, evaluateNumberInput, parseBooleanText, parseClipboardGrid, parseDateText, parseNumberText, pasteGrid,
} from "../../lib/grid-paste"
import { ROW_KEY, fillColumn } from "../../lib/table-part-rows"

test("clipboard text from a spreadsheet splits into rows and cells, quotes included", () => {
    expect(parseClipboardGrid("Гвозди\t5\r\nШурупы\t3\r\n")).toEqual([["Гвозди", "5"], ["Шурупы", "3"]])
    expect(parseClipboardGrid('"Болт ""М8"""\t2\n"две\nстроки"\t1')).toEqual([['Болт "М8"', "2"], ["две\nстроки", "1"]])
    expect(parseClipboardGrid("одно")).toEqual([["одно"]])
    expect(parseClipboardGrid("a\t\tc")).toEqual([["a", "", "c"]])
})

test("cell text converts by column type", () => {
    expect(parseNumberText("1 234,5")).toBe(1234.5)
    expect(parseNumberText("12a")).toBeNull()
    expect(parseBooleanText("Да")).toBe(true)
    expect(parseBooleanText("нет")).toBe(false)
    expect(parseDateText("31.12.2026")).toBe("2026-12-31")
    expect(parseDateText("31.02.2026")).toBeNull()
    const resolve = (_column: string, text: string) => (text === "P-1" ? { id: "p1", name: "Гвозди" } : null)
    const reference = { name: "productId", type: "reference", referenceType: "product" } as MetadataField
    expect(convertCellText(reference, "P-1", resolve)).toEqual({ value: "p1", display: "Гвозди" })
    expect(convertCellText(reference, "нет такого", resolve)).toBeNull()
    expect(convertCellText({ name: "quantity", type: "integer" } as MetadataField, "2,5", resolve)).toBeNull()
    expect(convertCellText({ name: "kind", type: "string", enumValues: [{ value: "in", label: "Приход" }] } as MetadataField, "приход", resolve)).toEqual({ value: "in" })
})

test("a number cell takes numbers, expressions and changes of its value", () => {
    expect(evaluateNumberInput("1 234,5", null, false)).toBe(1234.5)
    expect(evaluateNumberInput("500*4", null, true)).toBe(2000)
    expect(evaluateNumberInput("1,5*(2+3)", null, false)).toBe(7.5)
    expect(evaluateNumberInput("+5", 10, true)).toBe(15)
    expect(evaluateNumberInput("*2", 10, true)).toBe(20)
    expect(evaluateNumberInput("10/3", null, true)).toBe(3)
    expect(evaluateNumberInput("0.1+0.2", null, false)).toBe(0.3)
    expect(evaluateNumberInput("-5", 10, true)).toBe(-5)
    expect(evaluateNumberInput("abc", 10, true)).toBeNull()
})

test("a pasted grid lays over the lines from a cell and adds the lines it needs", () => {
    const columns = [
        { name: "productId", label: "Товар", type: "reference", referenceType: "product" },
        { name: "quantity", label: "Количество", type: "integer" },
        { name: "batch", label: "Партия", type: "string", readOnly: true },
    ] as MetadataField[]
    const rows: JsonObject[] = [{ lineId: "l1", productId: "p0", quantity: 1 }]
    const resolve = (_column: string, text: string) => ({ "P-1": { id: "p1", name: "Гвозди" }, "P-2": { id: "p2", name: "Шурупы" } }[text] ?? null)
    let n = 0
    const result = pasteGrid(rows, { line: 0, column: 0 }, [["P-1", "5", "B-1"], ["P-2", "", ""], ["X-9", "2"]], columns,
        (column) => !column.readOnly, (column, text) => convertCellText(column, text, resolve), () => ({ [ROW_KEY]: `t-${++n}` }))
    expect(result.rows).toEqual([
        { lineId: "l1", productId: "p1", product: { id: "p1", name: "Гвозди" }, quantity: 5 },
        { [ROW_KEY]: "t-1", productId: "p2", product: { id: "p2", name: "Шурупы" } },
        { [ROW_KEY]: "t-2", quantity: 2 },
    ])
    expect(result.touched).toEqual(["l1", "t-1", "t-2"])
    expect(result.failures).toEqual([{ line: 2, column: "Товар", text: "X-9" }])
})

test("fill gives the selected lines the source line's value with its display", () => {
    const rows: JsonObject[] = [
        { lineId: "a", warehouseId: "w1", warehouse: { id: "w1", name: "Основной" } },
        { lineId: "b", warehouseId: "w2" },
        { lineId: "c" },
    ]
    expect(fillColumn(rows, new Set(["b", "c"]), rows[0], "warehouseId").slice(1)).toEqual([
        { lineId: "b", warehouseId: "w1", warehouse: { id: "w1", name: "Основной" } },
        { lineId: "c", warehouseId: "w1", warehouse: { id: "w1", name: "Основной" } },
    ])
})

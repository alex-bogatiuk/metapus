import { test, expect } from "@playwright/test"
import { fieldSummaryExtractor, formatSummaryNumber, isSummableField, numericOperand, summarize, summaryCopyText, summaryGroupText } from "../../lib/selection-summary"
import { EMPTY_ROW_SELECTION, clickRow, keepExisting, rowMoveTarget, rowRange, summaryRows } from "../../lib/row-selection"
import { mergeListColumns } from "../../lib/metadata-columns"
import type { MetadataField } from "../../lib/schemas/metadata"

test("totals are exact decimals: sum keeps the scale, average adds up to two places", () => {
  const summary = summarize([{ value: "0.1", unit: "" }, { value: "0.2", unit: "" }, null, { value: "10", unit: "" }])
  expect(summary).toEqual({ rows: 4, groups: [{ unit: "", count: 3, sum: "10.3", average: "3.433" }] })
  expect(summarize([{ value: "10", unit: "" }, { value: "5", unit: "" }])!.groups[0].average).toBe("7.5")
  expect(summarize([null, null])).toBeNull()
  expect(summarize([{ value: "99999999999999999999.99", unit: "" }, { value: "0.01", unit: "" }])!.groups[0].sum).toBe("100000000000000000000.00")
})

test("values of different units are never added together", () => {
  const summary = summarize([{ value: "10", unit: "USDT" }, { value: "3", unit: "TRX" }, { value: "5", unit: "USDT" }])
  expect(summary!.groups).toEqual([
    { unit: "USDT", count: 2, sum: "15", average: "7.5" },
    { unit: "TRX", count: 1, sum: "3", average: "3" },
  ])
})

test("a total reads with groups and copies plain for a calculator", () => {
  expect(formatSummaryNumber("-1234567.50")).toBe("-1 234 567,50")
  expect(summaryCopyText("-1234567.50")).toBe("-1234567,50")
  expect(numericOperand("12.5")).toEqual({ value: "12.5", unit: "" })
  expect(numericOperand("00A")).toBeNull()
  expect(numericOperand(3)).toEqual({ value: "3", unit: "" })
})

test("money is summed in major units of the row's currency; numbers take their paired token", () => {
  const money = fieldSummaryExtractor({ name: "amount", type: "money" }, "amount")!
  expect(money({ amount: "12345", currency: { decimalPlaces: 2, symbol: "₽" } })).toEqual({ value: "123.45", unit: "₽" })
  expect(money({ amount: "12345" })).toBeNull()
  const columns = mergeListColumns({
    entityName: "Probe", pairAnyUnit: true,
    fields: [{ name: "expectedAmount", type: "decimal" }, { name: "tokenId", type: "reference", referenceType: "token" }, { name: "code", type: "string" }] as MetadataField[],
  })
  const expected = columns.find((column) => column.key === "expectedAmount")!
  expect(expected.summary!({ expectedAmount: "5", token: { id: "t", name: "Tether", code: "USDT" } })).toEqual({ value: "5", unit: "USDT" })
  expect(columns.find((column) => column.key === "code")!.summary).toBeUndefined()
})

test("rows are selected by click, Shift+click range and Cmd/Ctrl+click toggle", () => {
  const keys = ["a", "b", "c", "d", "e"]
  let state = clickRow(EMPTY_ROW_SELECTION, keys, "b", { shift: false, toggle: false })
  expect(summaryRows(state)).toBeNull()
  state = clickRow(state, keys, "d", { shift: true, toggle: false })
  expect(state).toEqual({ selected: ["b", "c", "d"], current: "d", anchor: "b" })
  state = clickRow(state, keys, "a", { shift: true, toggle: false })
  expect(state.selected).toEqual(["a", "b"])
  state = clickRow(state, keys, "e", { shift: false, toggle: true })
  expect(state.selected).toEqual(["a", "b", "e"])
  expect(summaryRows(state)).toEqual(["a", "b", "e"])
  state = clickRow(state, keys, "e", { shift: false, toggle: true })
  expect(summaryRows(state)).toBeNull()
  expect(keepExisting({ selected: ["a", "x"], current: "x", anchor: "a" }, keys)).toEqual({ selected: ["a"], current: null, anchor: "a" })
})

test("arrow, page and Home/End keys move the current row within the rows", () => {
    expect(rowMoveTarget("ArrowDown", 2, 5, 3)).toBe(3)
    expect(rowMoveTarget("ArrowDown", 4, 5, 3)).toBe(4)
    expect(rowMoveTarget("ArrowUp", 0, 5, 3)).toBe(0)
    expect(rowMoveTarget("PageDown", 1, 5, 3)).toBe(4)
    expect(rowMoveTarget("PageUp", 4, 5, 3)).toBe(1)
    expect(rowMoveTarget("PageUp", 1, 5, 3)).toBe(0)
    expect(rowMoveTarget("Home", 3, 5, 3)).toBe(0)
    expect(rowMoveTarget("End", 0, 5, 3)).toBe(4)
    // Without a current row the first step lands on an edge.
    expect(rowMoveTarget("ArrowDown", -1, 5, 3)).toBe(0)
    expect(rowMoveTarget("ArrowUp", -1, 5, 3)).toBe(4)
    expect(rowMoveTarget("ArrowLeft", 2, 5, 3)).toBeNull()
    expect(rowMoveTarget("ArrowDown", -1, 0, 3)).toBeNull()

    const keys = ["a", "b", "c", "d"]
    expect(rowRange(keys, "b", "d")).toEqual(["b", "c", "d"])
    expect(rowRange(keys, "c", "a")).toEqual(["a", "b", "c"])
    expect(rowRange(keys, "gone", "c")).toEqual(["c"])
})

test("the footer totals quantities and amounts, not prices or rates", () => {
    expect(isSummableField({ name: "quantity", type: "integer" })).toBe(true)
    expect(isSummableField({ name: "amount", type: "money" })).toBe(true)
    expect(isSummableField({ name: "discountAmount", type: "decimal" })).toBe(true)
    expect(isSummableField({ name: "price", type: "money" })).toBe(false)
    expect(isSummableField({ name: "vatRate", type: "decimal" })).toBe(false)
    expect(isSummableField({ name: "discountPercent", type: "number" })).toBe(false)
    expect(isSummableField({ name: "comment", type: "string" })).toBe(false)
    expect(summaryGroupText({ unit: "USDT", count: 2, sum: "1234.5", average: "617.25" })).toBe("1\u00a0234,5 USDT")
    expect(summaryGroupText({ unit: "", count: 2, sum: "26", average: "13" })).toBe("26")
})

import { test, expect } from "@playwright/test"
import { z } from "zod"
import { formRequestParts, projectFormRequest, registerHTTPRoutes } from "../../lib/http-contracts"
import {
  LineInputError, ROW_KEY, addRow, checkRequiredCells, copyRows, isEditableColumn, moveRow, moveRows, referenceDisplayName, removeRow, removeRows,
  restoreRow, rowKey, setCell,
} from "../../lib/table-part-rows"
import type { HTTPRouteContract } from "../../lib/http-contract-types"
import type { MetadataField } from "../../lib/schemas/metadata"
import type { JsonObject } from "../../lib/json-value"

const line = z.object({ lineId: z.string().nullable().optional(), productId: z.string(), quantity: z.number().optional() }).strict()
const receiptRoutes: HTTPRouteContract[] = [
  {
    scope: "api", method: "POST", path: "/document/probe-receipts",
    request: z.object({ date: z.string(), items: z.array(line).optional() }).strict(),
    requestFields: ["date", "items"], requestParts: { items: ["lineId", "productId", "quantity"] },
    response: null, optionalRequest: false,
  },
  {
    scope: "api", method: "PUT", path: "/document/probe-receipts/:id",
    request: z.object({ date: z.string(), items: z.array(line).optional(), version: z.number() }).strict(),
    requestFields: ["date", "items", "version"], requestParts: { items: ["lineId", "productId", "quantity"] },
    response: null, optionalRequest: false,
  },
]
registerHTTPRoutes(receiptRoutes)

test("a form request sends each line with the fields its line contract accepts", () => {
  const payload = projectFormRequest("PUT", "/document/probe-receipts/42", {
    date: "2026-11-17T09:00:00Z", version: 3, posted: false,
    items: [
      { lineId: "l-1", lineNo: 1, productId: "p-1", quantity: 2, batch: "B-1", product: { id: "p-1", name: "Гвозди" } },
      { [ROW_KEY]: "new-1", lineId: null, productId: "p-2", quantity: 1 },
    ],
  })
  expect(payload).toEqual({
    date: "2026-11-17T09:00:00Z", version: 3,
    items: [{ lineId: "l-1", productId: "p-1", quantity: 2 }, { productId: "p-2", quantity: 1 }],
  })
  expect(formRequestParts("POST", "/document/probe-receipts")).toEqual({ items: ["lineId", "productId", "quantity"] })
  expect(formRequestParts("PUT", "/document/unknown/1")).toEqual({})
})

test("only accepted, writable line fields are editable", () => {
  const quantity = { name: "quantity", type: "integer" } as MetadataField
  const batch = { name: "batch", type: "string", readOnly: true } as MetadataField
  const lineNo = { name: "lineNo", type: "integer" } as MetadataField
  const writable = ["lineId", "productId", "quantity", "lineNo"]
  expect(isEditableColumn(quantity, writable, false)).toBe(true)
  expect(isEditableColumn(quantity, writable, true)).toBe(false)
  expect(isEditableColumn(quantity, null, false)).toBe(false)
  expect(isEditableColumn(batch, [...writable, "batch"], false)).toBe(false)
  expect(isEditableColumn(lineNo, writable, false)).toBe(false)
})

test("rows are added, edited, moved and removed without touching the others", () => {
  let rows = addRow(addRow([{ lineId: "l-1", productId: "p-1", product: { id: "p-1", name: "Гвозди" } }]))
  expect(rows).toHaveLength(3)
  const keys = rows.map(rowKey)
  expect(new Set(keys).size).toBe(3)
  expect(keys[0]).toBe("l-1")

  rows = setCell(rows, 1, "productId", "p-2", "Шурупы")
  expect(referenceDisplayName(rows[1], "productId")).toBe("Шурупы")
  expect(referenceDisplayName(rows[0], "productId")).toBe("Гвозди")
  rows = setCell(rows, 1, "quantity", 5)
  expect(rows[1].quantity).toBe(5)

  rows = moveRow(rows, 1, -1)
  expect(rows.map(rowKey)).toEqual([keys[1], "l-1", keys[2]])
  expect(moveRow(rows, 0, -1)).toBe(rows)
  rows = removeRow(rows, 2)
  expect(rows.map(rowKey)).toEqual([keys[1], "l-1"])
})

test("an empty required cell of a writable part is named before saving", () => {
  const goods = { name: "goods", label: "Товары", columns: [
    { name: "productId", label: "Товар", type: "reference", required: true },
    { name: "quantity", label: "Количество", type: "integer" },
  ] as MetadataField[] }
  const writable = { goods: ["lineId", "productId", "quantity"] }
  expect(() => checkRequiredCells([goods], { goods: [{ productId: "p-1" }, { productId: "", quantity: 2 }] }, writable))
    .toThrow("Товары, строка 2: заполните «Товар»")
  expect(() => checkRequiredCells([goods], { goods: [{ productId: "p-1" }] }, writable)).not.toThrow()
  expect(() => checkRequiredCells([goods], { goods: [{}] }, {}), "a part the request does not accept is not checked").not.toThrow()
  expect(() => checkRequiredCells([goods], { goods: [{}] }, writable)).toThrow(LineInputError)
  // The error names the cell, so the form can put the user right into it.
  try {
    checkRequiredCells([goods], { goods: [{ productId: "p-1" }, { quantity: 2 }] }, writable)
  } catch (error) {
    expect(error).toMatchObject({ part: "goods", line: 1, column: "productId" })
  }
})

test("F9 copies the selected lines after the last of them as new lines with only writable fields", () => {
    const rows: JsonObject[] = [
        { lineId: "l1", lineNo: 1, productId: "p1", product: { id: "p1", name: "Гвозди" }, quantity: 2, batch: "B-7" },
        { lineId: "l2", lineNo: 2, productId: "p2", product: { id: "p2", name: "Шурупы" }, quantity: 3 },
        { lineId: "l3", lineNo: 3, productId: "p1", quantity: 7 },
    ]
    const { rows: next, copied } = copyRows(rows, new Set(["l1", "l2"]), ["productId", "quantity"])
    expect(copied).toHaveLength(2)
    expect(next.map((row, index) => rowKey(row, index))).toEqual(["l1", "l2", ...copied, "l3"])
    // A copy is a new line: no identity, no server-owned batch, the reference display kept.
    expect(next[2]).toEqual({ [ROW_KEY]: copied[0], productId: "p1", product: { id: "p1", name: "Гвозди" }, quantity: 2 })
    expect(copyRows(rows, new Set(["gone"]), ["quantity"]).copied).toEqual([])
})

test("lines are removed by key and an edit is cancelled by restoring the stored row", () => {
    const rows: JsonObject[] = [{ lineId: "a", quantity: 1 }, { lineId: "b", quantity: 2 }, { lineId: "c", quantity: 3 }]
    expect(removeRows(rows, new Set(["a", "c"]))).toEqual([{ lineId: "b", quantity: 2 }])
    const edited = setCell(rows, 1, "quantity", 20)
    expect(restoreRow(edited, "b", rows[1])).toEqual(rows)
})

test("selected lines move up and down as a block and stop at the edge", () => {
    const rows: JsonObject[] = ["a", "b", "c", "d", "e"].map((lineId) => ({ lineId }))
    const ids = (list: JsonObject[] | null) => list?.map((row) => row.lineId)
    expect(ids(moveRows(rows, new Set(["b", "d"]), -1))).toEqual(["b", "a", "d", "c", "e"])
    expect(ids(moveRows(rows, new Set(["c", "d"]), 1))).toEqual(["a", "b", "e", "c", "d"])
    expect(moveRows(rows, new Set(["a", "c"]), -1)).toBeNull()
    expect(moveRows(rows, new Set(["e"]), 1)).toBeNull()
    expect(moveRows(rows, new Set(), 1)).toBeNull()
})

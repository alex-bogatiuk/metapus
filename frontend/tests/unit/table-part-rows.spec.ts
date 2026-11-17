import { test, expect } from "@playwright/test"
import { z } from "zod"
import { formRequestParts, projectFormRequest, registerHTTPRoutes } from "../../lib/http-contracts"
import {
  LineInputError, ROW_KEY, addRow, checkRequiredCells, isEditableColumn, moveRow, referenceDisplayName, removeRow, rowKey, setCell,
} from "../../lib/table-part-rows"
import type { HTTPRouteContract } from "../../lib/http-contract-types"
import type { MetadataField } from "../../lib/schemas/metadata"

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
})

import { expect, test } from "@playwright/test"
import { CurrencyResponseSchema, CounterpartyResponseSchema } from "../../lib/generated/http-contract-schemas"
import { CreateCryptoInvoiceRequestSchema } from "../../configuration/crypto/lib/generated/http-contract-schemas"
import { cursorListSchema } from "../../lib/schemas/documents"

test("generated schemas distinguish omitted, nullable and required zero-valued fields", () => {
  const currency = { id: "id", code: "X", name: "Name", isoCode: null, symbol: null, decimalPlaces: 0, isBase: false, isFolder: false, deletionMark: false, version: 0 }
  expect(CurrencyResponseSchema.parse(currency)).toEqual(currency)
  expect(CurrencyResponseSchema.safeParse({ ...currency, decimalPlaces: "0" }).success).toBe(false)
  expect(CurrencyResponseSchema.safeParse({ ...currency, version: undefined }).success).toBe(false)
  expect(CurrencyResponseSchema.safeParse({ ...currency, parentId: null }).success).toBe(false)
  const list = cursorListSchema(CurrencyResponseSchema)
  expect(list.safeParse({ items: [currency], hasMore: false, hasPrev: false, totalCount: null }).success).toBe(true)
  expect(list.safeParse({ items: [{ ...currency, decimalPlaces: "2" }], hasMore: false, hasPrev: false, totalCount: 1 }).success).toBe(false)
})

test("generated request schemas check monetary fields and strict object before serialization", () => {
  const request = { date: "2026-09-13T00:00:00Z", merchantId: "merchant-1", tokenId: "token-1", expectedAmount: "9007199254740993" }
  expect(JSON.stringify(CreateCryptoInvoiceRequestSchema.parse(request))).toContain('"expectedAmount":"9007199254740993"')
  expect(CreateCryptoInvoiceRequestSchema.safeParse({ ...request, injectedField: true }).success).toBe(false)
  expect(CreateCryptoInvoiceRequestSchema.safeParse({ ...request, description: "Payment for order" }).success).toBe(true)
})

test("generated catalog projections accept FLS zero values without inventing an enum choice", () => {
  const masked = { id: "id", code: "", name: "Visible", type: "", legalForm: "", fullName: "", inn: "", isFolder: false, deletionMark: false, version: 1 }
  expect(CounterpartyResponseSchema.parse(masked)).toEqual(masked)
  expect(CounterpartyResponseSchema.safeParse({ ...masked, type: 1 }).success).toBe(false)
})

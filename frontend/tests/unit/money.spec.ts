import { test, expect } from "@playwright/test"
import { minorUnits, minorUnitsSchema, majorToMinor, minorToMajor, rawMinorToMajor, formatRawMinor, calculateLineMoney } from "../../lib/money"
import { moneyTotals, formatReportMoney, validateReportMoney } from "../../lib/report-money"
import type { ReportColumnDef } from "../../types/report-meta"
import { movementMoneyTotal, formatMovementMoney, validateMovementMoney } from "../../lib/movement-money"
import type { DocumentMovement, MovementColumnDef } from "../../types/common"

test("crypto report rows and totals keep arbitrary precision separately from int64 ERP", () => {
  const money = { amountType: "raw", currencyId: "uid", currencyCode: "code", decimalPlaces: "dp" } as const
  const column: ReportColumnDef = { key: "amount", label: "Сумма", type: "money", money }
  const raw = "9".repeat(78)
  const rows = [{ amount: raw, uid: "eth", code: "ETH", dp: 18 }, { amount: "2", uid: "eth", code: "ETH", dp: 18 }]
  validateReportMoney(rows, [column])
  expect(formatReportMoney(rows[0], column)).toContain(",999999999999999999 ETH")
  expect(moneyTotals(rows, { column: "amount", func: "sum", money })).toEqual([
    { amount: (BigInt(raw) + 2n).toString(), currencyId: "eth", currencyCode: "ETH", decimalPlaces: 18 },
  ])
  expect(() => validateReportMoney(rows, [{ ...column, money: { currencyId: "uid", currencyCode: "code", decimalPlaces: "dp" } }])).toThrow()
  expect(() => validateReportMoney([{ ...rows[0], amount: "1.1" }], [column])).toThrow()
})

test("minor units round-trip at 2^53 and int64 boundaries for every currency scale", () => {
  for (const raw of ["0", "-1", "9007199254740991", "9007199254740992", "9007199254740993", "9223372036854775807", "-9223372036854775808"]) {
    for (const scale of [0, 2, 3, 9, 18]) {
      expect(majorToMinor(minorToMajor(minorUnits(raw), scale), scale)).toBe(raw)
    }
  }
  expect(formatRawMinor("-9223372036854775808", 9)).toBe("-9\u00a0223\u00a0372\u00a0036,854775808")
  expect(formatRawMinor("-1", 2, "comma")).toBe("-0.01")
  expect(rawMinorToMajor("1000000000000000000000000000", 9)).toBe("1000000000000000000.000000000")
})

test("movement amounts use authorized row currency and never sum different units", () => {
  const column: MovementColumnDef = { key: "amount", label: "Сумма", type: "amount", currencyRef: "currency" }
  const row = (amount: string, code: string, scale: number): DocumentMovement => ({ registerName: "cost", recordType: "receipt", period: "2026-01-01", columns: [column], data: { amount, currency: { id: code, name: code, currencyCode: code, decimalPlaces: scale } } })
  const rows = [row("9007199254740993", "USD", 2), row("2", "USD", 2), row("123", "TON", 9)]
  validateMovementMoney(rows)
  expect(formatMovementMoney(rows[2], column)).toBe("0,000000123 TON")
  const total = movementMoneyTotal(rows, column, "sum")
  expect(total).toContain("90\u00a0071\u00a0992\u00a0547\u00a0409,95 USD")
  expect(total).toContain("0,000000123 TON")
  expect(() => validateMovementMoney([{ ...rows[0], data: { amount: "123" } }])).toThrow()
  expect(formatMovementMoney({ ...rows[0], data: {} }, column)).toBe("—")
})

test("wire numbers, ambiguous integers, overflow and excess user precision are rejected", () => {
  for (const input of [1, null, true, {}, "1.0", "1e2", "01", "-0", " 1", "+1", "9223372036854775808", "-9223372036854775809"]) {
    expect(minorUnitsSchema.safeParse(input).success).toBe(false)
  }
  for (const input of ["1e2", "1.001", "1x", "", "0.000", "92233720368547758.08"]) {
    expect(() => majorToMinor(input, 2)).toThrow()
  }
  expect(majorToMinor("1,23", 2)).toBe("123")
})

test("line totals preserve backend half-away rounding and discount/VAT order", () => {
  const line = { quantity: "0.5", unitPrice: "0.01", discountPercent: "0", vatPercent: "0" }
  expect(calculateLineMoney(line, true, 2)).toEqual({ amount: "1", vatAmount: "0" })
  expect(calculateLineMoney({ ...line, quantity: "-0.5" }, true, 2).amount).toBe("-1")
  expect(calculateLineMoney({ ...line, quantity: "1", unitPrice: "90071992547409.93" }, true, 2).amount).toBe("9007199254740993")
  const discount = { quantity: "3", unitPrice: "1.23", discountPercent: "10", vatPercent: "20" }
  expect(calculateLineMoney(discount, true, 2)).toEqual({ amount: "332", vatAmount: "55" })
  expect(calculateLineMoney(discount, false, 2)).toEqual({ amount: "398", vatAmount: "66" })
  expect(() => calculateLineMoney({ ...line, quantity: "2", unitPrice: "92233720368547758.07" }, true, 2)).toThrow()
})

test("report cells and totals keep separate currencies and exact large sums", () => {
  const money = { currencyId: "uid", currencyCode: "code", decimalPlaces: "dp" }
  const column: ReportColumnDef = { key: "amount", label: "Сумма", type: "money", money }
  const rows = [
    { amount: "9007199254740993", uid: "usd", code: "USD", dp: 2 },
    { amount: "2", uid: "usd", code: "USD", dp: 2 },
    { amount: "1001", uid: "kwd", code: "KWD", dp: 3 },
  ]
  validateReportMoney(rows, [column])
  expect(formatReportMoney(rows[0], column)).toBe("90\u00a0071\u00a0992\u00a0547\u00a0409,93 USD")
  expect(moneyTotals(rows, { column: "amount", func: "sum", money })).toEqual([
    { amount: "9007199254740995", currencyId: "usd", currencyCode: "USD", decimalPlaces: 2 },
    { amount: "1001", currencyId: "kwd", currencyCode: "KWD", decimalPlaces: 3 },
  ])
  expect(() => validateReportMoney([{ ...rows[0], amount: 123 }], [column])).toThrow()
  expect(() => validateReportMoney([{ ...rows[0], dp: undefined }], [column])).toThrow()
  expect(() => moneyTotals([...rows, { ...rows[0], dp: 3 }], { column: "amount", func: "sum", money })).toThrow()
  expect(formatReportMoney({ amount: null }, column)).toBe("—")
})

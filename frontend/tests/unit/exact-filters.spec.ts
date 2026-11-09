import { test, expect } from "@playwright/test"
import { buildFilterItems } from "../../lib/filter-utils"
import { exactFilterEntryError } from "../../lib/filter-numbers"
import { extractFilterValue, isColumnFilterValueReady } from "../../lib/list-column-filter"
import type { FilterFieldMeta } from "../../types/filter"

const money: FilterFieldMeta = { key: "totalAmount", label: "Сумма", fieldType: "money" }
const quantity: FilterFieldMeta = { key: "quantity", label: "Количество", fieldType: "number", valueScale: 10000 }

test("filter values keep exact money strings across a clicked cell, ranges and HTTP JSON", () => {
    const amount = "9007199254740993"
    expect(extractFilterValue({ totalAmount: amount }, "totalAmount", money)).toBe(amount)
    const entry = { operator: "between", value: { from: "-9223372036854775808", to: "9223372036854775807" } } as const
    expect(exactFilterEntryError(money, entry)).toBeUndefined()
    expect(isColumnFilterValueReady(money, entry.value, entry.operator)).toBe(true)
    const items = buildFilterItems({ totalAmount: entry }, [money])
    expect(JSON.parse(JSON.stringify(items))).toEqual([
        { field: "total_amount", fieldType: "money", operator: "gte", value: entry.value.from },
        { field: "total_amount", fieldType: "money", operator: "lte", value: entry.value.to },
    ])
    for (const value of [9007199254740992, "1.2", "1e2", "01", "-0", "9223372036854775808"]) {
        expect(exactFilterEntryError(money, { operator: "eq", value })).toBeTruthy()
        expect(isColumnFilterValueReady(money, value, "eq")).toBe(false)
    }
})

test("quantity filters preserve decimals and reject precision or range loss", () => {
    for (const value of ["-922337203685477.5808", "922337203685477.5807", "0.0001"]) {
        expect(exactFilterEntryError(quantity, { operator: "eq", value })).toBeUndefined()
        expect(extractFilterValue({ quantity: value }, "quantity", quantity)).toBe(value)
        expect(buildFilterItems({ quantity: { operator: "eq", value } }, [quantity])[0].value).toBe(value)
    }
    for (const value of ["922337203685477.5808", "0.00001", "1e2", ".1", "1.", " 1"]) {
        expect(exactFilterEntryError(quantity, { operator: "eq", value })).toBeTruthy()
        expect(isColumnFilterValueReady(quantity, value, "eq")).toBe(false)
    }
    expect(exactFilterEntryError(money, { operator: "null" })).toBeUndefined()
})

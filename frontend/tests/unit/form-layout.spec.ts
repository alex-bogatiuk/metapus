import { test, expect } from "@playwright/test"
import { AUTO_GROUP_LIMIT, alignPairsToRows, displayValue, pairAmountsWithUnits, planFormLayout } from "../../lib/form-layout"
import type { MetadataField } from "../../lib/schemas/metadata"

const f = (name: string, type: MetadataField["type"], extra: Partial<MetadataField> = {}) => ({ name, type, ...extra }) as MetadataField
const names = (fields: MetadataField[]) => fields.map((field) => field.name)

test("a short document keeps one untitled group, its identity row and inline lines", () => {
  const layout = planFormLayout({
    kind: "document", partCount: 1, accepted: null,
    fields: [f("number", "string"), f("date", "datetime"), f("warehouse", "string"), f("supplierId", "reference", { referenceType: "counterparty", required: true }), f("comment", "string")],
  })
  expect(names(layout.header)).toEqual(["number", "date"])
  expect(layout.groups).toHaveLength(1)
  expect(layout.groups[0].title).toBeUndefined()
  expect(names(layout.groups[0].fields)).toEqual(["supplierId", "warehouse"])
  expect(names(layout.wide)).toEqual(["comment"])
  expect(layout.partsMode).toBe("inline")
})

test("fields the form cannot write move to extra; required ones never do", () => {
  const layout = planFormLayout({
    kind: "document", partCount: 0, accepted: new Set(["number", "date", "amount", "mustHave"]),
    fields: [f("number", "string"), f("date", "datetime"), f("amount", "money"), f("txHash", "string"), f("checkedAt", "datetime", { readOnly: true }), f("mustHave", "string", { required: true, readOnly: true })],
  })
  expect(names(layout.extra)).toEqual(["txHash", "checkedAt"])
  expect(names(layout.groups.flatMap((group) => group.fields))).toEqual(["mustHave", "amount"])
})

test("an amount keeps its currency or token beside it", () => {
  const fields = [f("amount", "money"), f("note", "string"), f("feeAmount", "integer"), f("tokenId", "reference", { referenceType: "token" }), f("feeTokenId", "reference", { referenceType: "token" })]
  expect(names(pairAmountsWithUnits(fields))).toEqual(["amount", "tokenId", "note", "feeAmount", "feeTokenId"])
})

test("a long form is split by kind and one-field kinds join the main group", () => {
  const fields = [
    f("number", "string"), f("date", "datetime"),
    f("merchantId", "reference", { referenceType: "merchant", required: true }),
    f("walletId", "reference", { referenceType: "wallet" }),
    f("amount", "integer"), f("tokenId", "reference", { referenceType: "token" }),
    f("expiresAt", "datetime"), f("paidAt", "datetime"),
    f("externalId", "string"), f("callbackUrl", "string"),
    f("testMode", "boolean"), f("status", "enum"),
  ]
  expect(fields.length - 2).toBeGreaterThan(AUTO_GROUP_LIMIT)
  const layout = planFormLayout({ kind: "document", fields, partCount: 0, accepted: null })
  expect(layout.groups.map((group) => group.title)).toEqual(["Основное", "Суммы", "Даты", "Реквизиты"])
  expect(names(layout.groups[0].fields)).toEqual(["merchantId", "walletId", "testMode"])
  expect(names(layout.groups[1].fields)).toEqual(["amount", "tokenId"])
  expect(names(layout.groups[3].fields)).toEqual(["externalId", "callbackUrl", "status"])
})

test("explicit groups override the rules and follow the automatic ones", () => {
  const layout = planFormLayout({
    kind: "catalog", partCount: 0, accepted: null,
    fields: [f("code", "string"), f("name", "string", { required: true }), f("inn", "string", { group: "Реквизиты" }), f("kpp", "string", { group: "Реквизиты" }), f("phone", "string"), f("note", "string", { group: "Реквизиты", readOnly: true })],
  })
  expect(names(layout.header)).toEqual(["code", "name"])
  expect(layout.groups.map((group) => group.title)).toEqual(["Основное", "Реквизиты"])
  expect(names(layout.groups[1].fields)).toEqual(["inn", "kpp", "note"])
  expect(layout.extra).toEqual([])
})

test("table parts go on tabs when there are several or the form is long", () => {
  const short = [f("number", "string"), f("date", "datetime")]
  expect(planFormLayout({ kind: "document", fields: short, partCount: 2, accepted: null }).partsMode).toBe("tabs")
  expect(planFormLayout({ kind: "document", fields: short, partCount: 1, accepted: null }).partsMode).toBe("inline")
  const long = [...short, ...Array.from({ length: 11 }, (_, i) => f(`field${i}`, "string"))]
  expect(planFormLayout({ kind: "document", fields: long, partCount: 1, accepted: null }).partsMode).toBe("tabs")
})

test("an amount and its unit share a row of the two-column grid", () => {
  const fields = [f("merchantId", "reference", { referenceType: "merchant" }), f("expectedAmount", "decimal"), f("tokenId", "reference", { referenceType: "token" }), f("expiresAt", "datetime"), f("orderId", "string")]
  const layout = planFormLayout({ kind: "document", fields, partCount: 0, accepted: null })
  expect(names(layout.groups[0].fields)).toEqual(["merchantId", "expiresAt", "expectedAmount", "tokenId", "orderId"])
  expect(names(alignPairsToRows([f("expectedAmount", "decimal"), f("tokenId", "reference", { referenceType: "token" })]))).toEqual(["expectedAmount", "tokenId"])
})

test("server-owned values are shown as text", () => {
  expect(displayValue(f("paid", "boolean"), true)).toBe("Да")
  expect(displayValue(f("txHash", "string"), "")).toBe("—")
  expect(displayValue(f("status", "enum", { enumValues: [{ value: "paid", label: "Оплачен" }] } as Partial<MetadataField>), "paid")).toBe("Оплачен")
  expect(displayValue(f("paidAt", "date"), "2026-11-17T00:00:00Z")).toMatch(/2026/)
})

test("a catalog pairs an amount only with a unit named like it", () => {
  const fields = [f("sweepKeepAmount", "integer"), f("currencyId", "reference", { referenceType: "currency" }), f("feeAmount", "integer"), f("feeTokenId", "reference", { referenceType: "token" })]
  expect(names(pairAmountsWithUnits(fields, false))).toEqual(["sweepKeepAmount", "currencyId", "feeAmount", "feeTokenId"])
  expect(names(pairAmountsWithUnits([fields[1], fields[0]], false))).toEqual(["currencyId", "sweepKeepAmount"])
})

test("a calculated field stays among its inputs instead of going to Дополнительно", () => {
    const fields = [
        { name: "number", type: "string" },
        { name: "date", type: "date" },
        { name: "base", label: "База", type: "money" },
        { name: "fee", label: "Комиссия", type: "money", readOnly: true, calc: "base*rate/100" },
        { name: "postedAt", label: "Проведён", type: "datetime", readOnly: true },
    ] as MetadataField[]
    const layout = planFormLayout({ kind: "document", fields, partCount: 0, accepted: new Set(["number", "date", "base"]) })
    expect(layout.groups.flatMap((group) => group.fields.map((field) => field.name))).toEqual(["base", "fee"])
    expect(layout.extra.map((field) => field.name)).toEqual(["postedAt"])
})

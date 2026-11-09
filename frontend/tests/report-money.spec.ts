import { expect, test, type Page, type Route } from "@playwright/test"
import { commonAPIResponse, ids, installSession, json, tokenRecord, tokenRequiredFields } from "./helpers"
import { jsonObjectSchema, type JsonObject } from "../lib/json-value"

const reportPath = "/api/v1/reports/document-journal"
const sources = { currencyId: "currency_id", currencyCode: "currency", decimalPlaces: "currency_decimal_places" }
const metadata: JsonObject = {
  key: "document-journal", name: "Точные суммы", filters: [{ key: "amount", label: "Сумма фильтра", type: "string" }],
  columns: [{ key: "number", label: "Документ", type: "string" }, { key: "amount", label: "Сумма", type: "money", money: sources }],
  groupBy: [{ key: "currency", label: "Валюта" }], totals: [{ column: "amount", func: "sum", money: sources }],
}
const rows = [
  { number: "Scale 0", amount: "9007199254740993", currency_id: "JPY", currency: "JPY", currency_decimal_places: 0 },
  { number: "Scale 2", amount: "9007199254740993", currency_id: "USD", currency: "USD", currency_decimal_places: 2 },
  { number: "Scale 3", amount: "9007199254740993", currency_id: "KWD", currency: "KWD", currency_decimal_places: 3 },
  { number: "Scale 9", amount: "9007199254740993", currency_id: "TON", currency: "TON", currency_decimal_places: 9 },
]

async function setup(page: Page, handle: (route: Route, url: URL) => Promise<void>, reportMetadata: JsonObject = metadata) {
  await installSession(page)
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url())
    const common = commonAPIResponse(url.pathname)
    if (common !== undefined) return json(route, common)
    if (url.pathname === `${reportPath}/metadata`) return json(route, reportMetadata)
    if (url.pathname.includes("variants")) return json(route, [])
    return handle(route, url)
  })
  await page.goto("/reports/document-journal")
  await expect(page.getByRole("heading", { name: "Точные суммы" })).toBeVisible()
}

test("report preserves amounts above 2^53 at every currency scale and keeps currency totals separate", async ({ page }) => {
  await setup(page, route => json(route, { items: rows, totalItems: rows.length }))
  await page.getByRole("button", { name: "Сформировать", exact: true }).click()
  const expected = ["9 007 199 254 740 993 JPY", "90 071 992 547 409,93 USD", "9 007 199 254 740,993 KWD", "9 007 199,254740993 TON"]
  for (let index = 0; index < rows.length; index++) {
    await expect(page.getByRole("row").filter({ hasText: rows[index].number })).toContainText(expected[index])
  }
  const footer = page.getByRole("row").filter({ hasText: "Итого" }).last()
  for (const value of expected) await expect(footer).toContainText(value)
})

test("crypto report uses token scale and exact values beyond int64", async ({ page }) => {
  const cryptoSources = { ...sources, amountType: "crypto" }
  const cryptoMetadata: JsonObject = { ...metadata,
    columns: [{ key: "number", label: "Документ", type: "string" }, { key: "amount", label: "Сумма", type: "money", money: cryptoSources }],
    totals: [{ column: "amount", func: "sum", money: cryptoSources }],
  }
  const items = [
    { number: "Crypto exact", amount: "100000000000000000001", currency_id: "eth", currency: "ETH", currency_decimal_places: 18 },
    { number: "Crypto increment", amount: "1", currency_id: "eth", currency: "ETH", currency_decimal_places: 18 },
  ]
  await setup(page, route => json(route, { items, totalItems: items.length }), cryptoMetadata)
  await page.getByRole("button", { name: "Сформировать", exact: true }).click()
  await expect(page.getByRole("row").filter({ hasText: "Crypto exact" })).toContainText("100,000000000000000001 ETH")
  await expect(page.getByRole("row").filter({ hasText: "Итого" }).last()).toContainText("100,000000000000000002 ETH")
})

test("server grouping and export use the generated query and superseded grouping cannot replace rows", async ({ page }) => {
  test.setTimeout(60_000)
  const items = Array.from({ length: 5001 }, (_, index) => ({ ...rows[1], number: `Документ ${index}`, amount: "2" }))
  const grouped: { method: string; body: JsonObject; groups: string[] }[] = []
  let pending: Route | null = null
  let exported: JsonObject | null = null
  await setup(page, async (route, url) => {
    if (url.pathname === `${reportPath}/grouped`) {
      grouped.push({ method: route.request().method(), body: jsonObjectSchema.parse(route.request().postDataJSON()), groups: url.searchParams.getAll("groupBy") })
      if (grouped.length === 1) { pending = route; return }
      return json(route, { rows: [{ kind: "group", label: "Новая группировка", count: 5001, totals: {} }], totalItems: 5001 })
    }
    if (url.pathname === `${reportPath}/export`) {
      exported = jsonObjectSchema.parse(route.request().postDataJSON())
      return route.fulfill({ contentType: "text/csv", headers: { "Content-Disposition": 'attachment; filename="report.csv"' }, body: "amount\n9007199254740993\n" })
    }
    return json(route, { items, totalItems: items.length })
  })
  await page.getByPlaceholder("Сумма фильтра").fill("9007199254740993")
  await page.getByRole("button", { name: "Сформировать", exact: true }).click()
  await expect.poll(() => grouped.length).toBe(1)
  await page.getByPlaceholder("Сумма фильтра").fill("2")
  await page.getByRole("button", { name: "Группировка" }).click()
  await page.getByRole("menuitemcheckbox", { name: "Валюта" }).click()
  await expect(page.getByRole("row", { name: "Новая группировка 5001" })).toBeVisible()
  expect(grouped[1]).toMatchObject({ method: "POST", groups: ["currency"], body: { filters: { amount: "9007199254740993" } } })
  expect(grouped[1].body).toEqual(grouped[0].body)
  // The old HTTP request has been cancelled. A server that finishes it anyway
  // must not publish its result into the current grouping.
  if (pending) await json(pending, { rows: [{ kind: "group", label: "Устаревшая группировка", count: 5001 }], totalItems: 5001 })
  const download = page.waitForEvent("download")
  await page.getByRole("button", { name: "Экспорт", exact: true }).click()
  await download
  expect(exported).toMatchObject({ filters: { amount: "9007199254740993" }, exportGroupBy: ["currency"] })
  expect(exported).not.toHaveProperty("limit")
  await expect(page.getByRole("row", { name: "Новая группировка 5001" })).toBeVisible()
  await expect(page.getByRole("row", { name: "Устаревшая группировка 5001" })).toHaveCount(0)
})

test("AutoForm validates and sends exact monetary strings", async ({ page }) => {
  await installSession(page)
  const writes: JsonObject[] = []
  const record = { ...tokenRecord, name: "Денежная карточка", sweepThreshold: "9007199254740993", version: 1 }
  const entityMeta = { key: "token", name: "Token", type: "catalog", routePrefix: "tokens", presentation: { singular: "Карточка", plural: "Карточки" },
    fields: [...tokenRequiredFields, { name: "name", label: "Наименование", type: "string" }, { name: "sweepThreshold", label: "Сумма", type: "money" }, { name: "version", type: "integer", readOnly: true }] }
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/meta/entities") return json(route, [entityMeta])
    if (path.startsWith("/api/v1/meta/")) return json(route, entityMeta)
    const common = commonAPIResponse(path)
    if (common !== undefined) return json(route, common)
    if (path !== `/api/v1/catalog/tokens/${ids.token}`) return json(route, { items: [], total: 0 })
    if (route.request().method() === "PUT") writes.push(jsonObjectSchema.parse(route.request().postDataJSON()))
    return json(route, { ...record, ...writes.at(-1) })
  })
  await page.goto(`/catalogs/tokens/${ids.token}`)
  const sweepThreshold = page.locator('label[for="sweepThreshold"]').locator("..").locator("input")
  await expect(sweepThreshold).toHaveValue("9007199254740993")
  await sweepThreshold.fill("9223372036854775808")
  await expect(sweepThreshold).toHaveAttribute("aria-invalid", "true")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect(page.getByRole("alert").filter({ hasText: "Введите целое число минимальных единиц в допустимом диапазоне" })).toBeVisible()
  expect(writes).toHaveLength(0)
  await sweepThreshold.fill("-9223372036854775808")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].sweepThreshold).toBe("-9223372036854775808")
})

test("AutoForm rejects a malformed monetary response without replacing its snapshot", async ({ page }) => {
  await installSession(page)
  let corrupt = false
  const entityMeta = { key: "token", name: "Token", type: "catalog", routePrefix: "tokens", presentation: { singular: "Карточка", plural: "Карточки" },
    fields: [...tokenRequiredFields, { name: "name", type: "string" }, { name: "sweepThreshold", label: "Сумма", type: "money" }] }
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/meta/entities") return json(route, [entityMeta])
    if (path.startsWith("/api/v1/meta/")) return json(route, entityMeta)
    const common = commonAPIResponse(path)
    if (common !== undefined) return json(route, common)
    if (path === `/api/v1/catalog/tokens/${ids.token}`) return json(route, { ...tokenRecord, name: "Карточка", version: corrupt ? 2 : 1, sweepThreshold: corrupt ? 123 : "9007199254740993" })
    return json(route, { items: [], total: 0 })
  })
  await page.goto(`/catalogs/tokens/${ids.token}`)
  const sweepThreshold = page.locator('label[for="sweepThreshold"]').locator("..").locator("input")
  await expect(sweepThreshold).toHaveValue("9007199254740993")
  corrupt = true
  await page.getByRole("button", { name: "Перезагрузить", exact: true }).click()
  await expect(page.getByRole("alert").filter({ hasText: "Не удалось обновить карточку" })).toBeVisible()
  await expect(sweepThreshold).toHaveValue("9007199254740993")
})

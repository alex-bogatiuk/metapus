import { expect, test, type Page, type Route } from "@playwright/test"
import type { JsonObject } from "../lib/json-value"
import { commonAPIResponse, ids, installSession, json } from "./helpers"

const targetId = ids.token
const currencyMeta = {
  key: "currency", name: "Currency", type: "catalog", routePrefix: "currencies",
  presentation: { singular: "Валюта", plural: "Валюты" },
  fields: [
    { name: "name", label: "Наименование", type: "string" },
    { name: "code", label: "Код", type: "string" },
    { name: "isoCode", label: "ISO Код", type: "string" },
    { name: "symbol", label: "Символ", type: "string" },
    { name: "version", type: "integer", readOnly: true },
  ],
}
const counterpartyMeta = {
  ...currencyMeta, key: "counterparty", name: "Counterparty", routePrefix: "counterparties",
  presentation: { singular: "Контрагент", plural: "Контрагенты" },
  fields: [{ name: "name", label: "Наименование", type: "string" }, { name: "code", label: "Код", type: "string" }],
}

async function setup(page: Page, kind: "currency" | "counterparty" = "currency", options: { hidden?: boolean; fresh?: boolean; failOnce?: boolean } = {}) {
  await installSession(page)
  const meta = kind === "currency" ? currencyMeta : counterpartyMeta
  const listPath = `/catalogs/${meta.routePrefix}`
  const apiPath = `/api/v1/catalog/${meta.routePrefix}`
  const initialFilters = { name: { operator: "contains", value: "Запись" } }
  let record: JsonObject = kind === "currency"
    ? { id: targetId, name: "Запись исходная", code: "CUR", isoCode: "CUR", symbol: "C", decimalPlaces: 2, isBase: false, isFolder: false, deletionMark: false, version: 1 }
    : { id: targetId, name: "Запись исходная", code: "CP", type: "supplier", legalForm: "company", fullName: null, inn: null, isFolder: false, deletionMark: false, version: 1 }
  let exists = !options.fresh
  let failOnce = options.failOnce
  const lists: URL[] = []
  let pending: Route | null = null
  let pendingSave: Route | null = null
  let holdNext = false
  let holdSave = false
  function rows() {
    const items = Array.from({ length: 80 }, (_, index) => ({
      ...record, id: `20000000-0000-4000-8000-${String(index + 1).padStart(12, "0")}`,
      name: `Запись ${String(index + 1).padStart(3, "0")}`,
    }))
    if (exists && !options.hidden) items.splice(50, 0, { ...record, id: targetId, name: String(record.name) })
    return items
  }
  await page.route("**/api/v1/**", async route => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === "/api/v1/meta/entities") return json(route, [meta])
    if (url.pathname.startsWith("/api/v1/meta/") && url.pathname.endsWith("/filters")) return json(route, [
      { key: "name", label: "Наименование", fieldType: "string" },
    ])
    if (url.pathname.startsWith("/api/v1/meta/")) return json(route, meta)
    if (url.pathname === "/api/v1/me/preferences") return json(route, {
      userId: ids.user, interface: { showDeletedEntities: { [meta.key]: true } },
      listFilters: { [meta.key]: initialFilters }, listColumns: {}, dashboardLayout: null,
      favorites: [], updatedAt: "2026-11-01T00:00:00Z",
    })
    const common = commonAPIResponse(url.pathname)
    if (common !== undefined) return json(route, common)
    if (url.pathname === `${apiPath}/${targetId}` || (url.pathname === apiPath && request.method() === "POST")) {
      if (request.method() !== "GET") {
        const body = request.postDataJSON() as JsonObject
        record = { ...record, ...body, id: targetId, version: Number(record.version) + 1 }
        if (kind === "counterparty") {
          for (const key of ["kpp", "ogrn", "legalAddress", "actualAddress", "phone", "email", "contactPerson", "comment"]) {
            if (record[key] === null) delete record[key]
          }
        }
        exists = true
        if (holdSave) { pendingSave = route; return }
      }
      return json(route, record)
    }
    if (url.pathname === apiPath) {
      lists.push(url)
      if (url.searchParams.has("around")) {
        if (holdNext) { holdNext = false; pending = route; return }
        if (failOnce) { failOnce = false; return json(route, { code: "INTERNAL_ERROR", message: "Временная ошибка списка" }, 500) }
        if (options.hidden) return json(route, { code: "NOT_FOUND", message: "Запись не найдена" }, 404)
        return json(route, { items: rows(), targetIndex: 50, totalCount: null, hasMore: false, hasPrev: false })
      }
      const items = rows()
      return json(route, { items: items.slice(0, Number(url.searchParams.get("limit") ?? 100)), totalCount: items.length, hasMore: false, hasPrev: false })
    }
    if (url.pathname.includes("list-views")) return json(route, [])
    return json(route, { items: [], total: 0 })
  })
  return {
    listPath, lists,
    aroundRequests: () => lists.filter(url => url.searchParams.has("around")),
    holdAround: () => { holdNext = true },
    hasPending: () => pending !== null,
    holdSave: () => { holdSave = true },
    hasPendingSave: () => pendingSave !== null,
    releaseSave: async () => { if (pendingSave) await json(pendingSave, record); pendingSave = null; holdSave = false },
    releaseAround: async () => {
      if (pending) await json(pending, { items: [{ ...record, name: "Устаревший ответ" }], targetIndex: 0, hasMore: false, hasPrev: false })
      pending = null
    },
  }
}

function autoField(page: Page, name: string) {
  return page.locator(`label[for="${name}"]`).locator("..").locator("input")
}
async function expectRevealed(page: Page, listPath: string) {
  await expect.poll(() => new URL(page.url()).pathname).toBe(listPath)
  await expect.poll(() => new URL(page.url()).searchParams.has("around")).toBe(false)
  const row = page.locator(`[data-row-id="${targetId}"]`)
  await expect(row).toHaveAttribute("data-focused", "true")
  await expect(row).toBeInViewport()
  await expect(row).toBeFocused()
  await expect.poll(async () => {
    const persisted = await page.evaluate(() => JSON.parse(localStorage.getItem("metapus-tabs") ?? "{}").state?.tabs)
    return persisted?.find((tab: { id: string; url: string }) => tab.id === listPath)?.url
  }).not.toContain("around=")
}

test("AutoForm reveals repeated saves in a cached list, preserving sort, search and filters despite MRU history", async ({ page }) => {
  const server = await setup(page)
  await page.goto(`${server.listPath}?sort=name&dir=desc&view=kept`)
  await expect(page.locator(`[data-row-id="${targetId}"]`)).toBeVisible()
  await page.getByPlaceholder("Поиск (Ctrl+F)").fill("Запись")
  await expect.poll(() => server.lists.at(-1)?.searchParams.get("search")).toBe("Запись")
  for (const revision of [1, 2]) {
    await page.locator(`[data-row-id="${targetId}"]`).dblclick()
    await expect(autoField(page, "name")).toBeVisible()
    await page.locator('[data-tab-id="/"]').click()
    await expect(page).toHaveURL("/")
    await page.locator(`[data-tab-id="${server.listPath}/${targetId}"]`).click()
    await expect(page).toHaveURL(`${server.listPath}/${targetId}`)
    await autoField(page, "name").fill(`Запись обновлена ${revision}`)
    await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
    await expectRevealed(page, server.listPath)
    await expect(page.locator(`[data-row-id="${targetId}"]`)).toContainText(`Запись обновлена ${revision}`)
    await expect(page).toHaveURL(`${server.listPath}?sort=name&dir=desc&view=kept`)
    await expect(page.getByPlaceholder("Поиск (Ctrl+F)")).toHaveValue("Запись")
    const around = server.aroundRequests().at(-1)!
    expect(around.searchParams.get("orderBy")).toBe("-name")
    expect(around.searchParams.get("search")).toBe("Запись")
    expect(around.searchParams.get("includeDeleted")).toBe("true")
    expect(JSON.parse(around.searchParams.get("filter")!)).toContainEqual({ field: "name", fieldType: "string", operator: "contains", value: "Запись" })
    await expect(page.locator(`[data-tab-id="${server.listPath}/${targetId}"]`)).toHaveCount(0)
  }
  expect(server.aroundRequests()).toHaveLength(2)
  await page.getByRole("button", { name: "Обновить", exact: true }).click()
  await expect.poll(() => server.lists.at(-1)?.searchParams.has("around")).toBe(false)
  expect(server.aroundRequests()).toHaveLength(2)
})

test("AutoForm create-and-close opens a missing list and consumes around without opening an edit tab", async ({ page }) => {
  const server = await setup(page, "currency", { fresh: true })
  await page.goto(`${server.listPath}/new`)
  for (const [name, value] of Object.entries({ name: "Запись создана", code: "NEW", isoCode: "NEW", symbol: "N" })) {
    await autoField(page, name).fill(value)
  }
  await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
  await expectRevealed(page, server.listPath)
  await expect(page.locator(`[data-row-id="${targetId}"]`)).toContainText("Запись создана")
  await expect(page.locator(`[data-tab-id="${server.listPath}/${targetId}"]`)).toHaveCount(0)
  await expect(page.locator(`[data-tab-id="${server.listPath}/new"]`)).toHaveCount(0)
})

test("AutoForm creation preserves input made during save-and-close in the new edit tab", async ({ page }) => {
  const server = await setup(page, "currency", { fresh: true })
  await page.goto(`${server.listPath}/new`)
  for (const [name, value] of Object.entries({ name: "Запись создана", code: "NEW", isoCode: "NEW", symbol: "N" })) {
    await autoField(page, name).fill(value)
  }
  server.holdSave()
  await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
  await expect.poll(server.hasPendingSave).toBe(true)
  await autoField(page, "code").fill("Ввод после отправки")
  await server.releaseSave()
  await expect(page).toHaveURL(`${server.listPath}/${targetId}`)
  await expect(autoField(page, "code")).toHaveValue("Ввод после отправки")
  await expect(page.locator(`[data-tab-id="${server.listPath}/${targetId}"]`)).toHaveAttribute("data-dirty", "true")
  expect(server.aroundRequests()).toHaveLength(0)
})

for (const create of [false, true]) {
  test(`useCatalogForm ${create ? "creates" : "updates"} and reveals the saved record`, async ({ page }) => {
    const server = await setup(page, "counterparty", { fresh: create })
    await page.goto(`${server.listPath}${create ? "/new" : `/${targetId}`}`)
    await page.getByLabel("Наименование *", { exact: true }).fill("Запись контрагента")
    await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
    await expectRevealed(page, server.listPath)
    await expect(page.locator(`[data-row-id="${targetId}"]`)).toContainText("Запись контрагента")
  })
}

test("a target excluded from the list keeps filters and falls back to fresh rows with an explanation", async ({ page }) => {
  const server = await setup(page, "currency", { hidden: true })
  await page.goto(`${server.listPath}/${targetId}`)
  await autoField(page, "name").fill("Другая валюта")
  await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
  await expect(page).toHaveURL(server.listPath)
  await expect(page.getByText("Не удалось показать запись в текущем списке. Возможно, она скрыта фильтрами или недоступна.", { exact: true })).toBeVisible()
  await expect(page.locator("tr[data-row-id]")).toHaveCount(80)
  await expect(page.locator('[data-focused="true"]')).toHaveCount(0)
  expect(JSON.parse(server.aroundRequests()[0].searchParams.get("filter")!)).toContainEqual({ field: "name", fieldType: "string", operator: "contains", value: "Запись" })
})

test("a transient around failure can be retried", async ({ page }) => {
  const server = await setup(page, "currency", { failOnce: true })
  await page.goto(`${server.listPath}?sort=name&dir=desc&around=${targetId}`)
  await expect(page.getByText("Временная ошибка списка", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Повторить", exact: true }).click()
  await expectRevealed(page, server.listPath)
  await expect(page).toHaveURL(`${server.listPath}?sort=name&dir=desc`)
})

test("an around failure on a cached list explains the failure and allows refresh to retry", async ({ page }) => {
  const server = await setup(page, "currency", { failOnce: true })
  await page.goto(server.listPath)
  await page.locator(`[data-row-id="${targetId}"]`).dblclick()
  await autoField(page, "name").fill("Запись после ошибки")
  await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
  await expect(page.getByText("Не удалось показать запись. Обновите список.", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Обновить", exact: true }).click()
  await expectRevealed(page, server.listPath)
  await expect(page.locator(`[data-row-id="${targetId}"]`)).toContainText("Запись после ошибки")
})

test("a late around response cannot overwrite a newer sort request", async ({ page }) => {
  const server = await setup(page)
  await page.goto(`${server.listPath}?sort=name&dir=desc`)
  await page.locator(`[data-row-id="${targetId}"]`).dblclick()
  await autoField(page, "name").fill("Запись актуальная")
  server.holdAround()
  await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
  await expect.poll(server.hasPending).toBe(true)
  await page.getByRole("columnheader", { name: /Наименование/ }).click()
  await expectRevealed(page, server.listPath)
  await expect(page).toHaveURL(`${server.listPath}?sort=name&dir=asc`)
  await server.releaseAround()
  await expect(page.getByText("Устаревший ответ", { exact: true })).toHaveCount(0)
  await expect(page.locator(`[data-row-id="${targetId}"]`)).toContainText("Запись актуальная")
})

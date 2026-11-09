import type { JsonObject } from "@/lib/json-value"
import { expect, test, type Page, type Route } from "@playwright/test"
import {
  commonAPIResponse,
  erpUser,
  ids,
  installPortalSession,
  installSession,
  json,
  portalOwner
} from "./helpers"

const usd = {
  id: "20000000-0000-4000-8000-000000000001",
  code: "USD",
  name: "Доллар США",
  decimalPlaces: 2
}
const usdt = {
  id: "20000000-0000-4000-8000-000000000002",
  code: "USDT",
  name: "Tether",
  decimalPlaces: 6
}
const source = {
  id: "20000000-0000-4000-8000-000000000003",
  code: "CG",
  name: "CoinGecko Demo",
  sourceType: "coingecko"
}
const quote = {
  id: "20000000-0000-4000-8000-000000000004",
  currency: usdt,
  baseCurrency: usd,
  source,
  rate: "0.999712345678912345",
  multiplier: 1,
  version: 3,
  mappingVersion: 2,
  feedAttemptId: ids.adjustment,
  providerUpdatedAt: "2026-09-05T12:00:00Z",
  receivedAt: "2026-09-05T12:00:01Z",
  recordedAt: "2026-09-05T12:00:02Z"
}
const at = "2026-09-05T12:05:00Z"
const until = "2026-09-05T12:15:00Z"
const current = {
  feedHealth: null,
  baseCurrency: usd,
  source,
  sourceActive: true,
  evaluatedAt: at,
  intervalSeconds: 300,
  maxAgeSeconds: 900,
  nextCursor: null,
  items: [
    {
      currency: usdt,
      baseCurrency: usd,
      status: "fresh",
      reason: "selected",
      selectedQuote: quote,
      lastKnownQuote: quote,
      evaluatedAt: at,
      freshUntil: until
    },
    {
      currency: usd,
      baseCurrency: usd,
      status: "identity",
      reason: "same_currency",
      selectedQuote: null,
      lastKnownQuote: null,
      evaluatedAt: at,
      freshUntil: null
    }
  ]
}
const balance = {
  baseCurrency: usd,
  source,
  evaluatedAt: at,
  freshUntil: until,
  valuation: {
    total: { amount: "999712345678912345.67", coverage: "partial", unvaluedCount: 1 },
    available: { amount: "999712345678912345.67", coverage: "complete", unvaluedCount: 0 },
    pending: { amount: null, coverage: "none", unvaluedCount: 1 }
  },
  byToken: [
    {
      tokenId: ids.token,
      tokenSymbol: "USDT",
      currencyCode: "USDT",
      decimalPlaces: 6,
      rawAmount: "100000000",
      humanAmount: "100",
      availableRaw: "100000000",
      availableHuman: "100",
      pendingRaw: "0",
      pendingHuman: "0",
      valuation: {
        status: "fresh",
        reason: "selected",
        selectedQuote: quote,
        lastKnownQuote: quote,
        freshUntil: until,
        total: "99.97",
        available: "99.97",
        pending: "0.00"
      }
    }
  ]
}

async function ratesRoutes(page: Page, permissions: string[]) {
  await installSession(page)
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: permissions })
    const common = commonAPIResponse(path)
    if (common !== undefined) return json(route, common)
    if (path.endsWith("/exchange-rates/current")) return json(route, current)
    if (path.endsWith("/exchange-rates/history"))
      return json(route, {
        items: [quote, { ...quote, id: ids.outflow, version: 2 }],
        from: "2026-09-05T00:00:00Z",
        to: "2026-09-06T00:00:00Z",
        nextCursor: null
      })
    if (path.endsWith(`/exchange-rates/${quote.id}`)) return json(route, quote)
    return json(route, { items: [] })
  })
}
test("ERP reader sees rates, URL filters and all history versions by keyboard", async ({
  page
}) => {
  await ratesRoutes(page, ["register:exchange_rate:read"])
  await page.goto("/finance/exchange-rates")
  await expect(page.getByRole("heading", { name: "Курсы валют", exact: true })).toBeVisible()
  await page.screenshot({ path: "test-results/exchange-rates-erp.png", fullPage: true })
  await page.getByRole("button", { name: "USDT", exact: true }).focus()
  await page.keyboard.press("Enter")
  await expect(page.getByRole("dialog")).toContainText("0,999712345678912345")
  await expect(page).toHaveURL(/details=/)
  await page.keyboard.press("Escape")
  await page.getByRole("button", { name: "История", exact: true }).first().click()
  await expect(page).toHaveURL(/history=/)
  await expect(page.getByRole("dialog").getByRole("row")).toHaveCount(3)
  await page
    .getByRole("dialog")
    .getByRole("button", { name: /05.09.2026/ })
    .first()
    .click()
  await expect(page).toHaveURL(new RegExp(`quote=${quote.id}`))
  await expect(page.getByRole("dialog")).toContainText("Версия соответствия: 2")
})
test("ERP user without register permission cannot load rates", async ({ page }) => {
  await ratesRoutes(page, [])
  let requests = 0
  page.on("request", (request) => {
    if (request.url().includes("/registers/exchange-rates")) requests++
  })
  await page.goto("/finance/exchange-rates")
  await expect(page.getByRole("alert").filter({ hasText: "Нет права" })).toContainText("Нет права")
  expect(requests).toBe(0)
})
test("Portal preserves precision and marks an expired failed refresh", async ({ page }) => {
  await page.clock.install({ time: new Date(at) })
  await installPortalSession(page)
  let calls = 0
  await page.route("**/api/v1/**", (route) =>
    json(
      route,
      commonAPIResponse(new URL(route.request().url()).pathname, portalOwner) ?? { items: [] }
    )
  )
  await page.route("**/portal/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith("/merchants"))
      return json(route, {
        items: [{ id: ids.merchant, name: "Acme", code: "ACME", role: 1, networkAccess: "allow" }]
      })
    if (path.endsWith("/balances") || path.endsWith("/dashboard/balance")) {
      calls++
      return calls === 1
        ? json(route, balance)
        : json(route, { message: "DB unavailable", code: "INTERNAL_ERROR" }, 500)
    }
    return json(route, { items: [] })
  })
  await page.goto("/portal")
  await expect(page.getByText("999 712 345 678 912 345,67", { exact: false }).first()).toBeVisible()
  await expect(page.getByText("Оценена часть баланса", { exact: false })).toBeVisible()
  const displayedAmount = page.getByText("999 712 345 678 912 345,67", { exact: false }).first()
  expect(await displayedAmount.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
  await page.screenshot({ path: "test-results/exchange-rates-portal.png", fullPage: true })
  await page.getByRole("button", { name: "О курсе USDT" }).click()
  await expect(page.getByRole("dialog")).toContainText("0,999712345678912345")
  await page.keyboard.press("Escape")
  await page.clock.fastForward(600001)
  await expect(
    page.getByRole("status").filter({ hasText: "Показанная оценка устарела" })
  ).toContainText("Не удалось обновить баланс")
  expect(calls).toBeGreaterThan(1)
})
test("mapping version conflict keeps input and close asks to discard", async ({ page }) => {
  const admin = { ...erpUser, isAdmin: true }
  await installSession(page, admin)
  let version = 1
  const meta = {
    name: "RateSource",
    type: "catalog",
    key: "rate_source",
    routePrefix: "rate-sources",
    presentation: { singular: "Источник курсов", plural: "Источники курсов" },
    fields: [
      { name: "name", type: "string", label: "Наименование" },
      { name: "sourceType", type: "string", label: "Тип источника" }
    ]
  }
  await page.route("**/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/meta/entities") return json(route, [meta])
    if (path.startsWith("/api/v1/meta/")) return json(route, meta)
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path.endsWith(`/rate-sources/${source.id}`))
      return json(route, { ...source, version: 1, rateLimitRpm: 30, isActive: true, apiKeyConfigured: false, priority: 0, isFolder: false, deletionMark: false })
    if (path.endsWith("/mappings"))
      return json(route, {
        baseCurrency: usd,
        items: [
          {
            currency: usdt,
            baseCurrency: usd,
            sourceId: source.id,
            externalId: "tether",
            isActive: true,
            version
          }
        ]
      })
    if (path.includes("/mappings/")) {
      version = 2
      return json(route, { code: "CONCURRENT_MODIFICATION", message: "conflict" }, 409)
    }
    if (path.endsWith("/feed-state"))
      return json(route, {
        status: "idle",
        lastAttemptAt: null,
        lastSuccessAt: null,
        requested: 0,
        received: 0,
        inserted: 0,
        repeated: 0,
        rejected: 0,
        problems: []
      })
    return json(route, { items: [] })
  })
  await page.goto(`/catalogs/rate-sources/${source.id}`)
  await page.getByRole("button", { name: "Изменить", exact: true }).click()
  await page.getByLabel("CoinGecko ID", { exact: true }).fill("usd-coin")
  await page.getByRole("button", { name: "Сохранить соответствие" }).click()
  await expect(page.getByRole("alert")).toContainText("Введённые данные сохранены")
  await expect(page.getByLabel("CoinGecko ID", { exact: true })).toHaveValue("usd-coin")
  await page.getByRole("button", { name: "Отмена", exact: true }).click()
  await expect(page.getByRole("alertdialog")).toContainText("Закрыть без сохранения?")
  await page.getByRole("button", { name: "Продолжить редактирование" }).click()
  await expect(page.getByLabel("CoinGecko ID", { exact: true })).toHaveValue("usd-coin")
})

test("rate source binds an integration pool without displaying legacy API keys", async ({ page }) => {
  const admin = { ...erpUser, isAdmin: true }
  await installSession(page, admin)
  let savedBody: JsonObject | null = null
  const meta = {
    name: "RateSource",
    type: "catalog",
    key: "rate_source",
    routePrefix: "rate-sources",
    presentation: { singular: "Источник курсов", plural: "Источники курсов" },
    fields: [
      { name: "name", type: "string", label: "Наименование" },
      { name: "sourceType", type: "string", label: "Тип источника" },
      { name: "baseUrl", type: "string", label: "Базовый URL" },
      { name: "priority", type: "integer", label: "Приоритет" },
      { name: "rateLimitRpm", type: "integer", label: "Лимит запросов/мин" },
      { name: "apiKey", type: "string", label: "Ключ API", writeOnly: true, listHidden: true },
      { name: "integrationPoolId", type: "reference", label: "Пул аккаунтов", referenceType: "integration_pool" },
      { name: "isActive", type: "boolean", label: "Активен" }
    ]
  }
  const response = (version: number) => ({
    ...source,
    baseUrl: "https://api.coingecko.com/api/v3",
    priority: 100,
    rateLimitRpm: 30,
    apiKeyConfigured: true,
    isActive: true,
    isFolder: false,
    deletionMark: false,
    version
  })
  const pool = { id: ids.wallet, code: "CG-POOL", name: "CoinGecko accounts", provider: "coingecko_demo", isActive: true, accountIds: [], version: 1, isFolder: false, deletionMark: false, attributes: {}, createdAt: at, updatedAt: at }
  await page.route("**/api/v1/**", (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path === "/api/v1/meta/entities") return json(route, [meta, { name: "IntegrationPool", key: "integration_pool", type: "catalog", routePrefix: "integration-pools", fields: [], presentation: { singular: "Пул", plural: "Пулы" } }])
    if (path.startsWith("/api/v1/meta/")) return json(route, meta)
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path.endsWith("/mappings")) return json(route, { baseCurrency: usd, items: [] })
    if (path.endsWith("/feed-state")) {
      return json(route, {
        status: "idle",
        lastAttemptAt: null,
        lastSuccessAt: null,
        requested: 0,
        received: 0,
        inserted: 0,
        repeated: 0,
        rejected: 0,
        problems: []
      })
    }
    if (path.endsWith(`/rate-sources/${source.id}`) && request.method() === "PUT") {
      savedBody = request.postDataJSON() as JsonObject
      const allowed = new Set(["name", "sourceType", "rateLimitRpm", "integrationPoolId", "isActive", "version"])
      if (Object.keys(savedBody).some((key) => !allowed.has(key))) {
        return json(route, { code: "VALIDATION_ERROR", message: "invalid request body" }, 400)
      }
      return json(route, response(2))
    }
    if (path.endsWith(`/rate-sources/${source.id}`)) return json(route, response(1))
    if (path.endsWith(`/integration-pools/${ids.wallet}`)) return json(route, pool)
    if (path.endsWith("/integration-pools")) return json(route, { items: [pool], hasMore: false, hasPrev: false, totalCount: 1 })
    return json(route, { items: [] })
  })

  await page.goto(`/catalogs/rate-sources/${source.id}`)
  await expect(page.getByLabel("Ключ API", { exact: true })).toHaveCount(0)
  const poolField = page.locator('label[for="integrationPoolId"]').locator("..").getByRole("combobox")
  await poolField.click()
  await page.getByRole("option", { name: /CoinGecko accounts/ }).click()
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => savedBody?.integrationPoolId).toBe(ids.wallet)
  expect(savedBody).not.toHaveProperty("apiKey")
  expect(savedBody).not.toHaveProperty("baseUrl")
  expect(savedBody).not.toHaveProperty("priority")
})

for (const [field, nextValue] of [["policyVersion", "2"], ["tenantId", "another-tenant"], ["sessionId", ids.wallet]] as const) {
  test(`SWR discards cached permissions for the same user after ${field} changes`, async ({ page, context }) => {
    await ratesRoutes(page, ["register:exchange_rate:read"])
    let holdPermissions = false
    const pending: Route[] = []
    let dataRequests = 0
    await page.route("**/api/v1/auth/me/permissions", route => {
      if (holdPermissions) { pending.push(route); return }
      return json(route, { items: ["register:exchange_rate:read"] })
    })
    await page.route("**/api/v1/**/exchange-rates/current**", route => {
      dataRequests++
      return json(route, current)
    })
    await page.goto("/finance/exchange-rates")
    await expect(page.getByRole("heading", { name: "Курсы валют", exact: true })).toBeVisible()
    await expect.poll(() => dataRequests).toBeGreaterThan(0)
    const requestsBeforeScopeChange = dataRequests
    const peer = await context.newPage()
    await peer.route("**/scope-peer", route => route.fulfill({ contentType: "text/html", body: "<title>Scope event peer</title>" }))
    await peer.goto(new URL("/scope-peer", page.url()).toString())
    holdPermissions = true
    await peer.evaluate(({ user, field, nextValue }) => {
      const cacheScope = { tenantId: "e2e-tenant", principalId: user.id, sessionId: "10000000-0000-4000-8000-000000000010", userVersion: "1", policyVersion: "1", [field]: nextValue }
      localStorage.setItem("metapus-auth", JSON.stringify({ state: {
        tokens: { accessToken: "new-scoped-token", expiresAt: "2099-01-01T00:00:00Z", tokenType: "Bearer", cacheScope },
        user, cacheScope, isAuthenticated: true, originalSession: null, isImpersonating: false,
      }, version: 0 }))
    }, { user: erpUser, field, nextValue })
    await expect.poll(() => pending.length).toBeGreaterThan(0)
    await expect(page.getByRole("status").filter({ hasText: "Проверка прав доступа" })).toBeVisible()
    await expect(page.getByRole("heading", { name: "Курсы валют", exact: true })).toHaveCount(0)
    for (const route of pending) await json(route, { items: [] })
    await expect(page.getByRole("alert").filter({ hasText: "Нет права на просмотр курсов валют" })).toBeVisible()
    expect(dataRequests).toBe(requestsBeforeScopeChange)
  })
}

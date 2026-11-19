import type { JsonObject } from "@/lib/json-value"
import { expect, test, type Page } from "@playwright/test"
import { commonAPIResponse, ids, installSession, json } from "./helpers"

import definitions from "./fixtures/integration-providers.json"

const accountA = { id: ids.user, code: "ALCHEMY-A", name: "Alchemy A", isFolder: false, deletionMark: false, version: 1, attributes: {}, createdAt: "2026-10-19T10:00:00Z", updatedAt: "2026-10-19T10:00:00Z", provider: "alchemy", externalAccountRef: "account-a", isActive: true, resetDay: 1, resetTimezone: "UTC", credentialsVersion: 1, hasCredentials: true, credentialsExpiresAt: null as string | null, manualPeriodStartedAt: null as string | null, manualPeriodVersion: 0 }
const accountB = { ...accountA, id: ids.merchant, code: "ALCHEMY-B", name: "Alchemy B", externalAccountRef: "account-b" }
const pool = { id: ids.wallet, code: "RPC", name: "Alchemy RPC", isFolder: false, deletionMark: false, version: 1, attributes: {}, createdAt: accountA.createdAt, updatedAt: accountA.updatedAt, provider: "alchemy", isActive: true, accountIds: [accountA.id, accountB.id] }
const listFields = [
  { name: "code", label: "Код", type: "string" },
  { name: "name", label: "Наименование", type: "string" },
  { name: "provider", label: "Провайдер", type: "enum" },
  { name: "isActive", label: "Активен", type: "boolean" },
]
const metadata = [
  { key: "integration_account", name: "IntegrationAccount", type: "catalog", routePrefix: "integration-accounts", presentation: { singular: "Аккаунт интеграции", plural: "Аккаунты интеграций" }, fields: listFields },
  { key: "integration_pool", name: "IntegrationPool", type: "catalog", routePrefix: "integration-pools", presentation: { singular: "Пул аккаунтов", plural: "Пулы аккаунтов" }, fields: listFields },
]
async function setup(page: Page, provider = "alchemy") {
  await installSession(page)
  const writes: JsonObject[] = []
  let first = provider === "drpc" ? { ...accountA, provider, credentialsExpiresAt: "2026-01-01T19:00:00Z", manualPeriodStartedAt: "2025-12-01T00:00:00Z", manualPeriodVersion: 1 } : provider === "chainstack" ? { ...accountA, provider, code: "CHAINSTACK-NILE-01", name: "Chainstack Nile — аккаунт 1" } : { ...accountA, provider }
  const second = provider === "chainstack" ? { ...accountB, provider, code: "CHAINSTACK-NILE-02", name: "Chainstack Nile — аккаунт 2" } : accountB
  const activePool = { ...pool, provider }
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/meta/entities") return json(route, metadata)
    if (path.startsWith("/api/v1/meta/") && path.endsWith("/filters")) return json(route, [])
    if (path.startsWith("/api/v1/meta/")) return json(route, path.includes("Pool") || path.includes("integration_pool") ? metadata[1] : metadata[0])
    if (path.startsWith("/api/v1/me/list-views/")) return json(route, [])
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["catalog:integration_account:read", "catalog:integration_account:create", "catalog:integration_account:update", "catalog:integration_pool:read", "catalog:integration_pool:create", "catalog:integration_pool:update"] })
    if (path === "/api/v1/system/integrations/providers") return json(route, definitions)
    const common = commonAPIResponse(path)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/catalog/integration-accounts") {
      if (route.request().method() === "POST") { const request = route.request().postDataJSON(); writes.push(request); const created = { ...first, ...request, id: writes.length === 1 ? first.id : second.id, hasCredentials: !!request.credentials?.keys?.length, credentials: undefined }; if (created.id === accountA.id) first = created; return json(route, created) }
      return json(route, { items: [first, second], hasMore: false, hasPrev: false, totalCount: 2 })
    }
    if (path === `/api/v1/catalog/integration-accounts/${accountA.id}`) {
      if (route.request().method() === "PUT") { const request = route.request().postDataJSON(); writes.push(request); first = { ...first, ...request, hasCredentials: request.credentials ? !!request.credentials.keys.length : first.hasCredentials, credentials: undefined, credentialsVersion: 2, version: first.version + 1 }; return json(route, first) }
      return json(route, first)
    }
    if (path === "/api/v1/catalog/integration-pools" && route.request().method() === "POST") { const request = route.request().postDataJSON(); writes.push(request); return json(route, { ...activePool, ...request }) }
    if (path === "/api/v1/catalog/integration-pools") return json(route, { items: [activePool], hasMore: false, hasPrev: false, totalCount: 1 })
    if (path === `/api/v1/catalog/integration-pools/${pool.id}`) {
      if (route.request().method() === "PUT") { const request = route.request().postDataJSON(); writes.push(request); return json(route, { ...pool, ...request, version: 2 }) }
      return json(route, activePool)
    }
    if (path.endsWith(`/integrations/pools/${pool.id}/status`)) return json(route, { poolId: pool.id, accountId: accountA.id, selectionVersion: 3, reason: "account_selected", accounts: [] })
    return json(route, { items: [], nextCursor: null })
  })
  return writes
}

async function chooseProvider(page: Page, provider: string) {
  await page.getByRole("radio", { name: "Доступ к блокчейну — RPC", exact: true }).click()
  await page.getByLabel("Провайдер", { exact: true }).click()
  await page.getByRole("option", { name: provider, exact: true }).click()
}

for (const editCopy of [false, true]) {
  test(`unsaved account copy restores after switching tabs ${editCopy ? "with edits" : "without edits"}`, async ({ page }) => {
    const writes = await setup(page)
    const listPath = "/catalogs/integration-accounts"
    const copyPath = `${listPath}/new`
    let sourceReads = 0
    page.on("request", request => {
      if (request.method() === "GET" && new URL(request.url()).pathname === `/api/v1/catalog/integration-accounts/${accountA.id}`) sourceReads++
    })

    await page.goto(listPath)
    await page.getByRole("cell", { name: accountA.name, exact: true }).click({ button: "right" })
    await page.getByRole("menuitem", { name: "Создать копию" }).click()
    await expect(page).toHaveURL(`${copyPath}?copyFrom=${accountA.id}`)
    const name = page.getByLabel("Наименование", { exact: true })
    await expect(name).toHaveValue(accountA.name)
    const expectedName = editCopy ? "Alchemy — незаписанная копия" : accountA.name
    if (editCopy) await name.fill(expectedName)
    const title = `Создание копии: ${expectedName} (Аккаунт интеграции)`
    const copyTab = page.locator(`[data-tab-id="${copyPath}"]`)
    const initialSourceReads = sourceReads
    expect(initialSourceReads).toBeGreaterThan(0)

    for (let visit = 0; visit < 2; visit++) {
      await page.locator(`[data-tab-id="${listPath}"]`).click()
      await expect(page).toHaveURL(listPath)
      await copyTab.click()
      await expect(page).toHaveURL(`${copyPath}?copyFrom=${accountA.id}`)
      await expect(name).toHaveValue(expectedName)
      await expect(name).toBeEditable()
      await expect(page.getByRole("heading", { name: title, exact: true })).toBeVisible()
      await expect(page.getByLabel("ID аккаунта у провайдера", { exact: true })).toHaveValue(accountA.externalAccountRef)
      await expect(copyTab).toHaveAttribute("data-dirty", "true")
    }

    expect(sourceReads).toBe(initialSourceReads)
    expect(writes).toHaveLength(0)
  })
}

test("new accounts require a purpose and only show providers of that purpose", async ({ page }, testInfo) => {
  await setup(page)
  await page.goto("/catalogs/integration-accounts/new")
  await expect(page.getByLabel("Провайдер", { exact: true })).toBeDisabled()
  await expect(page.getByLabel("Наименование", { exact: true })).toHaveCount(0)
  await expect(page.getByText("Ключи доступа", { exact: true })).toHaveCount(0)
  await expect(page.getByRole("radio", { name: "Уведомления", exact: true })).toHaveCount(0)
  await page.getByRole("radio", { name: "Курсы валют", exact: true }).click()
  await page.getByLabel("Провайдер", { exact: true }).click()
  await expect(page.getByRole("option")).toHaveCount(1)
  await expect(page.getByRole("option", { name: "CoinGecko Demo", exact: true })).toBeVisible()
  await page.keyboard.press("Escape")
  await chooseProvider(page, "Infura")
  await expect(page.getByLabel("Ключ Infura", { exact: true })).toBeVisible()
  await expect(page.getByText("Ежедневно, 00:00 UTC", { exact: true })).toBeVisible()
  await expect(page.getByLabel("День обновления квоты", { exact: true })).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath("purpose-rpc-infura.png"), fullPage: true })
})

test("Ankr Freemium creates a monthly account from a Sepolia endpoint", async ({ page }) => {
  const writes = await setup(page, "ankr")
  await page.goto("/catalogs/integration-accounts/new")
  await chooseProvider(page, "Ankr")
  await page.getByLabel("Наименование", { exact: true }).fill("Ankr Sepolia")
  await expect(page.getByLabel("День обновления квоты", { exact: true })).toBeVisible()
  const key = page.getByLabel("Ключ Ankr", { exact: true })
  await key.fill("https://rpc.ankr.com/eth/test-project-key")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect(key).toHaveAttribute("aria-invalid", "true")
  expect(writes).toHaveLength(0)
  await key.fill("https://rpc.ankr.com/eth_sepolia/test-project-key")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ provider: "ankr", credentials: { keys: [{ name: "sepolia", value: "test-project-key" }] } })
  expect(JSON.stringify(writes[0])).not.toContain("rpc.ankr.com")
})

test("changing purpose confirms clearing secrets and sends no duplicate account type", async ({ page }) => {
  const writes = await setup(page)
  await page.goto("/catalogs/integration-accounts/new")
  await chooseProvider(page, "Alchemy")
  await page.getByLabel("Наименование", { exact: true }).fill("CoinGecko для курсов")
  await page.getByLabel("Значение ключа", { exact: true }).fill("rpc-secret-in-memory")
  await page.getByRole("radio", { name: "Курсы валют", exact: true }).click()
  await expect(page.getByRole("alertdialog")).toContainText("Введённые ключи будут очищены")
  await page.getByRole("button", { name: "Продолжить редактирование", exact: true }).click()
  await expect(page.getByLabel("Значение ключа", { exact: true })).toHaveValue("rpc-secret-in-memory")
  await page.getByRole("radio", { name: "Курсы валют", exact: true }).click()
  await page.getByRole("button", { name: "Очистить ключи и продолжить", exact: true }).click()
  await expect(page.getByLabel("Провайдер", { exact: true })).toContainText("Выберите провайдера")
  await page.getByLabel("Провайдер", { exact: true }).click()
  await page.getByRole("option", { name: "CoinGecko Demo", exact: true }).click()
  await expect(page.getByLabel("Значение ключа", { exact: true })).toHaveValue("")
  await page.getByLabel("Значение ключа", { exact: true }).fill("coingecko-secret-in-memory")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ provider: "coingecko_demo", name: "CoinGecko для курсов" })
  expect(writes[0]).not.toHaveProperty("kind")
  expect(writes[0]).not.toHaveProperty("type")
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain("secret-in-memory")
})

test("saved legacy accounts retain their provider and show its purpose", async ({ page }) => {
  const writes = await setup(page, "telegram")
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await expect(page.getByLabel("Назначение интеграции", { exact: true })).toHaveValue("Уведомления")
  await expect(page.getByLabel("Провайдер", { exact: true })).toBeDisabled()
  await expect(page.getByText("Уведомления настраиваются в отдельной подсистеме автоматизации.", { exact: true })).toBeVisible()
  await page.getByLabel("Наименование", { exact: true }).fill("Существующий Telegram")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ provider: "telegram" })
  expect(writes[0]).not.toHaveProperty("credentials")
})

test("creation from a purpose link still requires an explicit provider", async ({ page }) => {
  await setup(page)
  await page.goto("/catalogs/integration-accounts/new?kind=exchange_rates")
  await expect(page.getByRole("radio", { name: "Курсы валют", exact: true })).toBeChecked()
  await expect(page.getByLabel("Провайдер", { exact: true })).toBeEnabled()
  await expect(page.getByLabel("Провайдер", { exact: true })).toContainText("Выберите провайдера")
})

test("purpose filter in the list sends a provider condition to the server", async ({ page }, testInfo) => {
  await setup(page)
  await page.addInitScript(() => localStorage.setItem("metapus-filter-sidebar-collapsed", "false"))
  const rateAccount = { ...accountB, name: "CoinGecko для курсов", provider: "coingecko_demo" }
  await page.route("**/api/v1/catalog/integration-accounts?*", route => {
    const filter = new URL(route.request().url()).searchParams.get("filter") ?? ""
    const items = filter.includes("coingecko_demo") ? [rateAccount] : [accountA, rateAccount]
    return json(route, { items, hasMore: false, hasPrev: false, totalCount: items.length })
  })
  const queries: string[] = []
  page.on("request", request => {
    const url = new URL(request.url())
    if (url.pathname === "/api/v1/catalog/integration-accounts") queries.push(url.searchParams.get("filter") ?? "")
  })
  await page.goto("/catalogs/integration-accounts")
  await expect(page.getByRole("columnheader", { name: "Назначение", exact: true })).toBeVisible()
  await expect(page.getByRole("cell", { name: "Alchemy A", exact: true })).toBeVisible()
  await page.getByRole("combobox", { name: "Назначение", exact: true }).click()
  await page.getByRole("option", { name: "Курсы валют", exact: true }).click()
  await page.getByRole("button", { name: "Применить фильтры", exact: true }).click()
  await expect.poll(() => queries.some(query => query.includes('"provider"') && query.includes('"coingecko_demo"') && !query.includes('"integrationKind"'))).toBe(true)
  await expect(page.getByRole("cell", { name: "Alchemy A", exact: true })).toHaveCount(0)
  await expect(page.getByRole("cell", { name: "CoinGecko для курсов", exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath("integration-purpose-list.png"), fullPage: true })
})

test("switching a new pool purpose preserves members until confirmation", async ({ page }) => {
  const writes = await setup(page)
  await page.goto("/catalogs/integration-pools/new")
  await chooseProvider(page, "Alchemy")
  await page.getByLabel("Добавить аккаунт", { exact: true }).click()
  await page.getByRole("option", { name: "Alchemy A", exact: true }).click()
  await page.getByRole("radio", { name: "Курсы валют", exact: true }).click()
  await expect(page.getByRole("alertdialog")).toContainText("Сами аккаунты останутся в справочнике")
  await page.getByRole("button", { name: "Продолжить редактирование", exact: true }).click()
  await expect(page.getByRole("cell", { name: "Alchemy A", exact: true })).toBeVisible()
  await page.getByRole("radio", { name: "Курсы валют", exact: true }).click()
  await page.getByRole("button", { name: "Очистить состав и продолжить", exact: true }).click()
  await expect(page.getByRole("cell", { name: "Alchemy A", exact: true })).toHaveCount(0)
  await expect(page.getByLabel("Провайдер", { exact: true })).toContainText("Выберите провайдера")
  expect(writes).toHaveLength(0)
})

test("two Chainstack accounts accept separate Nile tokens and can be ordered in one pool", async ({ page }) => {
  const writes = await setup(page, "chainstack")
  for (const index of [1, 2]) {
    await page.goto("/catalogs/integration-accounts/new")
    await chooseProvider(page, "Chainstack")
    await page.getByLabel("Наименование", { exact: true }).fill(`Chainstack Nile — аккаунт ${index}`)
    await page.getByRole("button", { name: "Служебный код", exact: true }).click()
    await page.getByLabel("Код", { exact: true }).fill(`CHAINSTACK-NILE-0${index}`)
    await expect(page.getByText("Введите только токен", { exact: false })).toBeVisible()
    await page.getByLabel("Имя ключа 1", { exact: true }).fill("nile")
    const secret = `chainstack-test-token-${index}`
    await page.getByLabel("Значение ключа", { exact: true }).fill(secret)
    await expect.poll(async () => page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain(secret)
    await page.getByRole("button", { name: "Записать", exact: true }).click()
    await expect.poll(() => writes.length).toBe(index)
    expect(writes[index - 1]).toMatchObject({ provider: "chainstack", code: `CHAINSTACK-NILE-0${index}`, credentials: { keys: [{ name: "nile", value: secret }] } })
  }
  await page.goto("/catalogs/integration-pools/new")
  await chooseProvider(page, "Chainstack")
  await page.getByLabel("Наименование", { exact: true }).fill("Chainstack Nile")
  for (const index of [1, 2]) {
    await page.getByLabel("Добавить аккаунт", { exact: true }).click()
    await page.getByRole("option", { name: `Chainstack Nile — аккаунт ${index}`, exact: true }).click()
  }
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(3)
  expect(writes[2]).toMatchObject({ provider: "chainstack", accountIds: [accountA.id, accountB.id] })
  await expect(page).toHaveURL(new RegExp(`/catalogs/integration-pools/${pool.id}$`))
  await expect(page.getByRole("cell", { name: "Chainstack Nile — аккаунт 1", exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Переместить ниже" }).first().click()
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(4)
  expect(writes[3]).toMatchObject({ provider: "chainstack", accountIds: [accountB.id, accountA.id] })
})

test("account key stays out of persisted drafts and is cleared after save", async ({ page }) => {
  const writes = await setup(page)
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await expect(page.getByLabel("Наименование", { exact: true })).toHaveValue("Alchemy A")
  await expect(page.getByLabel("Значение ключа", { exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Заменить ключи…", exact: true }).click()
  const secret = "only-in-request-not-in-browser-storage"
  await page.getByLabel("Значение ключа", { exact: true }).fill(secret)
  await expect.poll(async () => page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain(secret)
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ credentials: { keys: [{ name: "default", value: secret }] }, version: 1 })
  await expect(page.getByLabel("Значение ключа", { exact: true })).toHaveCount(0)
  await page.getByLabel("Наименование", { exact: true }).fill("Alchemy A renamed")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(2)
  expect(writes[1]).not.toHaveProperty("credentials")
  expect(writes[1]).toMatchObject({ version: 2 })
  await page.screenshot({ path: "test-results/integration-account.png", fullPage: true })
})

test("pool order can be changed by keyboard and saved with expected catalog version", async ({ page }) => {
  const writes = await setup(page)
  await page.goto(`/catalogs/integration-pools/${pool.id}`)
  await expect(page.getByRole("cell", { name: "Alchemy A", exact: true })).toBeVisible()
  const down = page.getByRole("button", { name: "Переместить ниже" }).first()
  await down.focus(); await page.keyboard.press("Enter")
  const rows = page.getByRole("row")
  await expect(rows.nth(1)).toContainText("Alchemy B")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ accountIds: [accountB.id, accountA.id], version: 1 })
  await page.screenshot({ path: "test-results/integration-pool.png", fullPage: true })
})

for (const form of [
  { name: "pool", path: `/catalogs/integration-pools/${pool.id}`, title: pool.name, lastControl: "Проверить подключение" },
  { name: "account", path: `/catalogs/integration-accounts/${accountA.id}`, title: accountA.name, lastControl: "Часовой пояс провайдера" },
]) {
  test(`${form.name} form scrolls to its last control while save actions stay visible`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 600 })
    await setup(page)
    await page.goto(form.path)
    const name = page.getByLabel("Наименование", { exact: true })
    await expect(name).toHaveValue(form.title)
    const lastControl = form.name === "pool"
      ? page.getByRole("button", { name: form.lastControl, exact: true })
      : page.getByLabel(form.lastControl, { exact: true })
    await expect(lastControl).not.toBeInViewport()
    await name.hover()
    await page.mouse.wheel(0, 3000)
    await expect(lastControl).toBeInViewport()
    await expect(page.getByRole("button", { name: "Записать", exact: true })).toBeInViewport()
    await page.screenshot({ path: testInfo.outputPath(`${form.name}-form-scrolled.png`) })
    await lastControl.hover()
    await page.mouse.wheel(0, -3000)
    await expect(name).toBeInViewport()
  })
}


test("GetBlock stores two role tokens in one account without persisting secrets in drafts", async ({ page }) => {
  const pageErrors: string[] = []
  page.on("pageerror", error => pageErrors.push(error.message))
  const writes = await setup(page, "getblock")
  await page.goto("/catalogs/integration-accounts/new")
  await chooseProvider(page, "Getblock.io")
  await page.getByLabel("Наименование", { exact: true }).fill("GetBlock Nile")
  await expect(page.getByLabel("Имя ключа 1", { exact: true })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Добавить ключ", exact: true })).toHaveCount(0)
  await page.getByLabel("Токен FullNode REST", { exact: true }).fill("https://shared.eu-central-1.getblock.io/getblock-fullnode-test-secret/wallet/getnowblock")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect(page.getByLabel("Токен Solidity REST", { exact: true })).toBeFocused()
  expect(writes).toHaveLength(0)
  await page.getByLabel("Токен Solidity REST", { exact: true }).fill("getblock-solidity-test-secret")
  await page.getByLabel("Токен FullNode REST", { exact: true }).evaluate(input => {
    const data = new DataTransfer()
    data.setData("text", "https://shared.eu-central-1.getblock.io.attacker.test/invalid-paste/")
    input.dispatchEvent(new ClipboardEvent("paste", { clipboardData: data, bubbles: true, cancelable: true }))
  })
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect(page.getByLabel("Токен FullNode REST", { exact: true })).toHaveAttribute("aria-invalid", "true")
  expect(writes).toHaveLength(0)
  await page.getByLabel("Токен FullNode REST", { exact: true }).fill("getblock-fullnode-test-secret")
  await expect.poll(async () => page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain("test-secret")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ provider: "getblock", credentials: { keys: [
    { name: "fullnode", value: "getblock-fullnode-test-secret" },
    { name: "solidity", value: "getblock-solidity-test-secret" },
  ] } })
  await expect(page.getByLabel("Токен FullNode REST", { exact: true })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Заменить оба токена…", exact: true })).toBeVisible()
  await page.screenshot({ path: "test-results/getblock-account.png", fullPage: true })
  expect(pageErrors).toEqual([])
})

test("key deletion is explicit, reversible before save, and sends an empty set only after save", async ({ page }) => {
  const writes = await setup(page)
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await page.getByRole("button", { name: "Удалить все ключи…", exact: true }).click()
  await expect(page.getByRole("alertdialog")).toContainText("Ключи у провайдера не удаляются")
  await page.getByRole("button", { name: "Удалить после записи", exact: true }).click()
  await expect(page.getByText("Ключи будут удалены после записи", { exact: true })).toBeVisible()
  expect(writes).toHaveLength(0)
  await page.getByRole("button", { name: "Отменить удаление", exact: true }).click()
  await expect(page.getByRole("button", { name: "Записать", exact: true })).toBeDisabled()
  await page.getByRole("button", { name: "Удалить все ключи…", exact: true }).click()
  await page.getByRole("button", { name: "Удалить после записи", exact: true }).click()
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ credentials: { keys: [] }, version: 1 })
  await expect(page.getByText("Ключи сохранены", { exact: true })).toHaveCount(0)
})

test("cancelling replacement preserves metadata edits and back navigation guards the draft", async ({ page }) => {
  const writes = await setup(page)
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await page.getByRole("button", { name: "Заменить ключи…", exact: true }).click()
  await page.getByLabel("Значение ключа", { exact: true }).fill("unsaved-test-token")
  await page.getByLabel("Наименование", { exact: true }).fill("Unsaved name")
  await page.getByRole("button", { name: "Отменить замену", exact: true }).click()
  await expect(page.getByRole("button", { name: "Записать", exact: true })).toBeEnabled()
  await page.getByRole("button", { name: "К списку", exact: true }).click()
  await expect(page.getByRole("alertdialog")).toContainText("Закрыть без записи?")
  await page.getByRole("button", { name: "Продолжить редактирование", exact: true }).click()
  await expect(page.getByRole("alertdialog", { includeHidden: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Отменить изменения", exact: true }).click()
  await page.getByRole("alertdialog").getByRole("button", { name: "Отменить изменения", exact: true }).click()
  await expect(page.getByLabel("Наименование", { exact: true })).toHaveValue("Alchemy A")
  await expect(page.getByRole("button", { name: "Записать", exact: true })).toBeDisabled()
  expect(writes).toHaveLength(0)
})

test("failed save keeps secret inputs for retry and never persists them", async ({ page }) => {
  const writes = await setup(page)
  let fail = true
  await page.route(`**/api/v1/catalog/integration-accounts/${accountA.id}`, route => {
    if (route.request().method() === "PUT" && fail) { fail = false; return json(route, { code: "CONCURRENT_MODIFICATION", message: "Запись изменена другим пользователем. Обновите карточку." }, 409) }
    return route.fallback()
  })
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await page.getByRole("button", { name: "Заменить ключи…", exact: true }).click()
  const input = page.getByLabel("Значение ключа", { exact: true })
  await input.fill("retry-test-token")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect(page.getByRole("alert").filter({ hasText: "Запись изменена другим пользователем" })).toBeVisible()
  await expect(input).toHaveValue("retry-test-token")
  expect(writes).toHaveLength(0)
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain("retry-test-token")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  await expect(input).toHaveCount(0)
})

test("pool linking preserves existing members and uses a freshly read version", async ({ page }) => {
  const writes = await setup(page)
  await page.route(`**/api/v1/catalog/integration-pools/${pool.id}`, route => route.request().method() === "GET"
    ? json(route, { ...pool, isActive: false, version: 7, accountIds: [accountB.id] })
    : route.fallback())
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await page.getByRole("button", { name: "Добавить в пул…", exact: true }).click()
  await page.getByRole("combobox", { name: "Пул аккаунтов", exact: true }).click()
  await page.getByRole("option", { name: pool.name, exact: false }).first().click()
  await page.getByRole("dialog").getByRole("button", { name: "Добавить аккаунт", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ accountIds: [accountB.id, accountA.id], version: 7, isActive: false, provider: "alchemy" })
  await expect(page.getByRole("link", { name: "Открыть пул", exact: true })).toHaveAttribute("href", `/catalogs/integration-pools/${pool.id}`)
})

test("mobile toolbar retains save-and-close and the form fits a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setup(page, "getblock")
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await expect(page.getByLabel("Наименование", { exact: true })).toHaveValue("Alchemy A")
  await page.getByLabel("Наименование", { exact: true }).fill("GetBlock Nile")
  await page.getByRole("button", { name: "Ещё", exact: true }).click()
  await expect(page.getByRole("menuitem", { name: "Записать и закрыть", exact: true })).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(page.getByRole("menuitem", { name: "Записать и закрыть", exact: true })).not.toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: "test-results/integration-account-mobile.png", fullPage: true })
})


test("dRPC creation saves a Sepolia key and its explicit UTC expiry together", async ({ page }, testInfo) => {
  const writes = await setup(page, "drpc")
  await page.goto("/catalogs/integration-accounts/new")
  await chooseProvider(page, "dRPC")
  await page.getByLabel("Наименование", { exact: true }).fill("dRPC резерв Sepolia")
  await page.getByLabel("Ключ dRPC", { exact: true }).fill("https://lb.drpc.live/sepolia/test-secret-only-in-memory")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect(page.getByText("Укажите новую дату окончания в будущем (UTC).", { exact: true })).toBeVisible()
  expect(writes).toHaveLength(0)
  await page.getByLabel("Действует до (UTC)", { exact: true }).fill("2099-11-14T19:00")
  await expect(page.getByLabel("День обновления квоты", { exact: true })).toHaveCount(0)
  await page.getByText("Срок действия ключа", { exact: true }).click()
  await expect(page.getByText("Укажите новую дату окончания в будущем (UTC).", { exact: true })).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath("drpc-create-desktop.png"), fullPage: true })
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ provider: "drpc", credentialsExpiresAt: "2099-11-14T19:00:00.000Z", credentials: { keys: [{ name: "sepolia", value: "test-secret-only-in-memory" }] } })
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain("test-secret-only-in-memory")
})

test("expired dRPC allows metadata edits and renewal cancellation restores its saved date", async ({ page }, testInfo) => {
  const writes = await setup(page, "drpc")
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  const expiry = page.getByLabel("Действует до (UTC)", { exact: true })
  await expect(page.getByText("Срок действия истёк", { exact: true })).toBeVisible()
  await expect(expiry).toHaveValue("2026-01-01T19:00")
  await expect(expiry).toHaveAttribute("readonly", "")
  await page.getByLabel("Наименование", { exact: true }).fill("Истёкший резерв")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ name: "Истёкший резерв", credentialsExpiresAt: "2026-01-01T19:00:00Z" })
  expect(writes[0]).not.toHaveProperty("credentials")
  await page.getByRole("button", { name: "Продлить ключ…", exact: true }).click()
  await page.getByLabel("Ключ dRPC", { exact: true }).fill("canceled-secret")
  await expiry.fill("2099-12-15T00:00")
  await page.getByRole("button", { name: "Отменить замену", exact: true }).click()
  await expect(expiry).toHaveValue("2026-01-01T19:00")
  await expect(page.getByLabel("Ключ dRPC", { exact: true })).toHaveCount(0)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: testInfo.outputPath("drpc-expired-mobile.png"), fullPage: true })
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain("canceled-secret")
})

test("dRPC renewal requires a new date and sends one atomic update", async ({ page }) => {
  const writes = await setup(page, "drpc")
  await page.goto(`/catalogs/integration-accounts/${accountA.id}`)
  await page.getByRole("button", { name: "Продлить ключ…", exact: true }).click()
  const expiry = page.getByLabel("Действует до (UTC)", { exact: true })
  await expect(expiry).toHaveValue("")
  await page.getByLabel("Ключ dRPC", { exact: true }).fill("renewed-secret")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect(page.getByText("Укажите новую дату окончания в будущем (UTC).", { exact: true })).toBeVisible()
  expect(writes).toHaveLength(0)
  await expiry.fill("2099-12-15T00:00")
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toMatchObject({ version: 1, credentialsExpiresAt: "2099-12-15T00:00:00.000Z", credentials: { keys: [{ name: "sepolia", value: "renewed-secret" }] } })
  await expect(page.getByText("Ключи сохранены", { exact: true })).toBeVisible()
  await expect(page.getByLabel("Ключ dRPC", { exact: true })).toHaveCount(0)
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage }))).not.toContain("renewed-secret")
})

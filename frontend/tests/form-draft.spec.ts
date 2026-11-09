import { expect, test, type Page, type Route } from "@playwright/test"
import { commonAPIResponse, erpUser, ids, installSession, json, tokenRecord, tokenRequiredFields } from "./helpers"

const uiPath = `/catalogs/tokens/${ids.token}`
const apiPath = `/api/v1/catalog/tokens/${ids.token}`
const metadata = { key: "token", name: "Token", type: "catalog", routePrefix: "tokens",
  presentation: { singular: "Тестовая карточка", plural: "Тестовые карточки" },
  fields: [...tokenRequiredFields, { name: "name", label: "Наименование", type: "string" }, { name: "code", label: "Комментарий", type: "string" }, { name: "version", type: "integer", readOnly: true }] }
function field(page: Page, name: string) { return page.locator(`label[for="${name}"]`).locator("..").locator("input") }
async function setup(page: Page) {
  await installSession(page)
  let record = { ...tokenRecord, name: "Сервер А", code: "Исходный", version: 1 }
  const writes: { name: string; code: string; version: number }[] = []
  let holdRead = false
  let holdWrite = false
  let pendingRead: Route | null = null
  let pendingWrite: Route | null = null
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url())
    if (url.pathname === "/api/v1/meta/entities") return json(route, [metadata])
    if (url.pathname.startsWith("/api/v1/meta/")) return json(route, metadata)
    const common = commonAPIResponse(url.pathname)
    if (common !== undefined) return json(route, common)
    if (url.pathname === apiPath) {
      if (route.request().method() === "PUT") {
        const body = route.request().postDataJSON()
        writes.push(body)
        record = { ...record, name: body.name, code: body.code, version: record.version + 1 }
        if (holdWrite) { pendingWrite = route; return }
      } else if (holdRead) { pendingRead = route; return }
      return json(route, record)
    }
    return json(route, { items: [], total: 0 })
  })
  await page.goto(uiPath)
  await expect(field(page, "name")).toHaveValue(record.name)
  return {
    writes,
    holdRead: () => { holdRead = true },
    holdWrite: () => { holdWrite = true },
    hasRead: () => pendingRead !== null,
    hasWrite: () => pendingWrite !== null,
    releaseRead: async (name = "Сервер изменился") => { record = { ...record, name, version: record.version + 1 }; if (pendingRead) await json(pendingRead, record); holdRead = false; pendingRead = null },
    releaseWrite: async () => { if (pendingWrite) await json(pendingWrite, record); holdWrite = false; pendingWrite = null },
  }
}

test("AutoForm keeps input made during GET and confirms destructive reload", async ({ page }) => {
  const server = await setup(page)
  server.holdRead()
  await page.getByRole("button", { name: "Перезагрузить", exact: true }).click()
  await expect.poll(server.hasRead).toBe(true)
  await field(page, "name").fill("Мой ввод во время GET")
  await server.releaseRead()
  await expect(field(page, "name")).toHaveValue("Мой ввод во время GET")
  await expect(page.getByText("Данные на сервере изменились", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Перезагрузить", exact: true }).click()
  const dialog = page.getByRole("alertdialog")
  await expect(dialog).toBeVisible()
  await dialog.getByRole("button", { name: "Отмена", exact: true }).click()
  await expect(dialog).toBeHidden()
  await expect(field(page, "name")).toHaveValue("Мой ввод во время GET")
  await page.getByRole("button", { name: "Перезагрузить", exact: true }).click()
  await dialog.getByRole("button", { name: "Перезагрузить", exact: true }).click()
  await expect(field(page, "name")).toHaveValue("Сервер изменился")
})

test("AutoForm keeps edits during save-and-close and sends the acknowledged version on retry", async ({ page }) => {
  const server = await setup(page)
  await field(page, "name").fill("Отправленное")
  server.holdWrite()
  await page.getByRole("button", { name: "Записать и закрыть", exact: true }).click()
  await expect.poll(server.hasWrite).toBe(true)
  await expect(field(page, "code")).toBeEnabled()
  await field(page, "code").fill("Ввод после отправки")
  await server.releaseWrite()
  await expect(field(page, "code")).toHaveValue("Ввод после отправки")
  await expect(page).toHaveURL(new RegExp(ids.token))
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => server.writes.length).toBe(2)
  expect(server.writes[1]).toMatchObject({ name: "Отправленное", code: "Ввод после отправки", version: 2 })
})

test("switching application tabs restores dirty draft while the remount GET is pending", async ({ page }) => {
  const server = await setup(page)
  await field(page, "name").fill("Черновик при переключении")
  await page.locator('[data-tab-id="/"]').click()
  await expect(page).toHaveURL(/\/$/)
  server.holdRead()
  await page.locator(`[data-tab-id="${uiPath}"]`).click()
  await expect.poll(server.hasRead).toBe(true)
  await expect(field(page, "name")).toHaveValue("Черновик при переключении")
  await server.releaseRead()
  await expect(field(page, "name")).toHaveValue("Черновик при переключении")
  await expect(page.locator(`[data-tab-id="${uiPath}"]`)).toHaveAttribute("data-dirty", "true")
})

test("AutoForm restores an unsaved copy without reloading its source on tab switches", async ({ page }) => {
  const server = await setup(page)
  const copyPath = "/catalogs/tokens/new"
  const copyUrl = `${copyPath}?copyFrom=${ids.token}`
  await page.goto(copyUrl)
  await expect(field(page, "name")).toHaveValue("Сервер А")
  await field(page, "name").fill("Незаписанная копия токена")
  server.holdRead()

  for (let visit = 0; visit < 2; visit++) {
    await page.locator('[data-tab-id="/"]').click()
    await expect(page).toHaveURL(/\/$/)
    await page.locator(`[data-tab-id="${copyPath}"]`).click()
    await expect(page).toHaveURL(copyUrl)
    await expect(field(page, "name")).toHaveValue("Незаписанная копия токена")
    await expect(field(page, "name")).toBeEditable()
    await expect(page.locator(`[data-tab-id="${copyPath}"]`)).toHaveAttribute("data-dirty", "true")
  }

  expect(server.hasRead()).toBe(false)
  expect(server.writes).toHaveLength(0)
})

test("another browser tab switches identity without showing cached drafts or old tab names", async ({ page, context }) => {
  await setup(page)
  await field(page, "name").fill("Секретный черновик А")
  const peer = await context.newPage()
  await peer.goto("/login")
  const nextUser = { ...erpUser, id: ids.merchant, fullName: "Пользователь Б" }
  // Real storage event from another page (no direct access to application stores).
  await page.route("**/api/v1/auth/me", route => json(route, nextUser))
  await page.route("**" + apiPath, route => json(route, { ...tokenRecord, name: "Данные Б", code: "", version: 5 }))
  await peer.evaluate(({ user, sessionId }) => {
    const cacheScope = { tenantId: "e2e-tenant", principalId: user.id, sessionId, userVersion: "1", policyVersion: "1" }
    localStorage.setItem("metapus-auth", JSON.stringify({ state: {
      tokens: { accessToken: "token-B", expiresAt: "2099-01-01T00:00:00Z", tokenType: "Bearer", cacheScope },
      user, cacheScope, isAuthenticated: true, originalSession: null, isImpersonating: false,
    }, version: 0 }))
  }, { user: nextUser, sessionId: ids.wallet })
  await expect(field(page, "name")).toHaveValue("Данные Б")
  await expect(page.getByText("Секретный черновик А", { exact: true })).toHaveCount(0)
  const persisted = await page.evaluate(() => localStorage.getItem("metapus-tabs"))
  expect(persisted).not.toContain("Секретный черновик А")
  await peer.evaluate(() => localStorage.removeItem("metapus-auth"))
  await expect(page).toHaveURL(/\/login$/)
})

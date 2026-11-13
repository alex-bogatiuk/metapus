import { expect, test, type Page } from "@playwright/test"
import { commonAPIResponse, ids, installSession, json } from "./helpers"

const meta = {
  key: "currency", name: "Currency", type: "catalog", routePrefix: "currencies",
  presentation: { singular: "Валюта", plural: "Валюты" },
  fields: [
    { name: "name", label: "Наименование", type: "string" },
    { name: "code", label: "Код", type: "string" },
    { name: "version", type: "integer", readOnly: true },
  ],
}
const row = { id: ids.token, name: "PayPal USD", code: "PYUSD", isoCode: "PYUSD", symbol: "$", decimalPlaces: 6, isBase: false, isFolder: false, deletionMark: false, version: 1 }

async function setup(page: Page) {
  await installSession(page)
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"])
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/meta/entities") return json(route, [meta])
    if (path.startsWith("/api/v1/meta/") && path.endsWith("/filters")) return json(route, [])
    if (path.startsWith("/api/v1/meta/")) return json(route, meta)
    const common = commonAPIResponse(path)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/catalog/currencies") return json(route, { items: [row], totalCount: 1, hasMore: false, hasPrev: false })
    if (path.includes("list-views")) return json(route, [])
    return json(route, { items: [], total: 0 })
  })
  await page.goto("/catalogs/currencies")
  await page.evaluate(() => navigator.clipboard.writeText("before"))
}

const clipboard = (page: Page) => page.evaluate(() => navigator.clipboard.readText())
const nameCell = (page: Page) => page.locator(`[data-row-id="${ids.token}"] td[data-column-key="name"]`)

test("Cmd/Ctrl+C copies the current list cell like 1C", async ({ page }) => {
  await setup(page)
  await nameCell(page).click()
  await expect(nameCell(page)).toHaveAttribute("data-current-cell", "true")
  await page.keyboard.press("ControlOrMeta+c")
  await expect.poll(() => clipboard(page)).toBe("PayPal USD")
})

test("a text selection keeps the native copy", async ({ page }) => {
  await setup(page)
  await nameCell(page).click()
  await nameCell(page).locator("span").evaluate(span => {
    const range = document.createRange()
    range.setStart(span.firstChild!, 0)
    range.setEnd(span.firstChild!, 6)
    document.getSelection()!.removeAllRanges()
    document.getSelection()!.addRange(range)
  })
  await page.keyboard.press("ControlOrMeta+c")
  await expect.poll(() => clipboard(page)).toBe("PayPal")
})

test("copying inside a search field is not taken over by the list", async ({ page }) => {
  await setup(page)
  await nameCell(page).click()
  const search = page.getByPlaceholder("Поиск (Ctrl+F)")
  await search.fill("query")
  await search.selectText()
  await page.keyboard.press("ControlOrMeta+c")
  await expect.poll(() => clipboard(page)).toBe("query")
})

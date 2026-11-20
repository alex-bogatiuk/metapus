import { expect, test, type Page } from "@playwright/test"
import { commonAPIResponse, installSession, json } from "./helpers"

const meta = {
  key: "currency", name: "Currency", type: "catalog", routePrefix: "currencies",
  presentation: { singular: "Валюта", plural: "Валюты" },
  fields: [{ name: "name", label: "Наименование", type: "string" }],
}
const listPath = "/catalogs/currencies"

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
    if (path === "/api/v1/catalog/currencies") return json(route, { items: [], totalCount: 0, hasMore: false, hasPrev: false })
    if (path.includes("list-views")) return json(route, [])
    return json(route, { items: [], total: 0 })
  })
  await page.goto(listPath)
  await expect(tab(page, listPath)).toHaveAttribute("aria-current", "page")
  await page.evaluate(() => navigator.clipboard.writeText("before"))
}

const tab = (page: Page, id: string) => page.locator(`[data-tab-id="${id}"]`)
const clipboard = (page: Page) => page.evaluate(() => navigator.clipboard.readText())

test("a single click on the open tab copies nothing", async ({ page }) => {
  await setup(page)
  await tab(page, listPath).click()
  await expect(page.getByText("Ссылка скопирована в буфер обмена")).toHaveCount(0)
  expect(await clipboard(page)).toBe("before")
})

test("a double click on the open tab copies its full link", async ({ page, baseURL }) => {
  await setup(page)
  await tab(page, listPath).dblclick()
  await expect.poll(() => clipboard(page)).toBe(`${baseURL}${listPath}`)
  await expect(page.getByText("Ссылка скопирована в буфер обмена")).toBeVisible()
})

test("a double click on a background tab only opens it", async ({ page, baseURL }) => {
  await setup(page)
  await tab(page, "/").dblclick()
  await expect(tab(page, "/")).toHaveAttribute("aria-current", "page")
  await expect(page.getByText("Ссылка скопирована в буфер обмена")).toHaveCount(0)
  expect(await clipboard(page)).toBe("before")

  // Once it is open, the next double click copies its link.
  await tab(page, "/").dblclick()
  await expect.poll(() => clipboard(page)).toBe(`${baseURL}/`)
})

import { expect, test, type Page } from "@playwright/test"
import { commonAPIResponse, installSession, json } from "./helpers"

async function setup(page: Page) {
  await installSession(page)
  // Tabs saved by an earlier visit, once: a reload must find what the app itself persisted.
  await page.addInitScript(() => {
    if (sessionStorage.getItem("tabs-seeded")) return
    sessionStorage.setItem("tabs-seeded", "1")
    localStorage.setItem("metapus-tabs", JSON.stringify({
      state: {
        tabs: [
          { id: "/", title: "Главное", url: "/" },
          { id: "/settings", title: "Настройки", url: "/settings" },
          // A title a reload guessed from the URL before this was fixed.
          { id: "/catalogs/integration-pools", title: "Справочники…", url: "/catalogs/integration-pools" },
        ],
        activeTabId: "/catalogs/integration-pools",
        activationHistory: ["/catalogs/integration-pools", "/settings", "/"],
      },
      version: 0,
    }))
  })
  await page.route("**/api/v1/**", async (route) => {
    const body = commonAPIResponse(new URL(route.request().url()).pathname)
    return body === undefined ? route.fulfill({ status: 404, json: { code: "NOT_FOUND", message: "not found" } }) : json(route, body)
  })
}

const tab = (page: Page, id: string) => page.locator(`[data-tab-id="${id}"]`)

test("a reload keeps the saved tabs and names each page as the navigation does", async ({ page }) => {
  await setup(page)
  await page.goto("/catalogs/integration-pools")
  await expect(tab(page, "/catalogs/integration-pools")).toContainText("Пулы аккаунтов")
  await expect(tab(page, "/settings")).toContainText("Настройки")

  await page.goto("/admin/crypto-networks")
  await expect(tab(page, "/admin/crypto-networks")).toContainText("Криптосети")

  await page.reload()
  await expect(tab(page, "/admin/crypto-networks")).toContainText("Криптосети")
  await expect(tab(page, "/catalogs/integration-pools")).toContainText("Пулы аккаунтов")
  await expect(tab(page, "/settings")).toContainText("Настройки")
  const saved = await page.evaluate(() => JSON.parse(localStorage.getItem("metapus-tabs") ?? "null")?.state?.tabs?.map((t: { id: string }) => t.id))
  expect(saved).toEqual(["/", "/settings", "/catalogs/integration-pools", "/admin/crypto-networks"])
})

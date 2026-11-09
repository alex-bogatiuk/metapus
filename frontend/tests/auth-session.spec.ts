import { expect, test, type BrowserContext } from "@playwright/test"
import { commonAPIResponse, ids, json, portalOwner, unvaluedPortalBalance } from "./helpers"

const admin = { ...portalOwner, isAdmin: true }
const cacheScope = { tenantId: "e2e-tenant", principalId: admin.id, sessionId: ids.wallet, userVersion: "1", policyVersion: "1" }
const tokens = { accessToken: "test-admin-access", expiresAt: "2099-01-01T00:00:00Z", tokenType: "Bearer", cacheScope }

async function setup(context: BrowserContext, refreshStatus: () => number) {
  await context.addInitScript(({ user, scope }) => {
    // Seed a browser restart: admin access tokens are deliberately memory-only.
    if (localStorage.getItem("auth-test-seeded")) return
    localStorage.setItem("auth-test-seeded", "true")
    localStorage.setItem("metapus-cache-scope", JSON.stringify(Object.values(scope)))
    localStorage.setItem("metapus-auth", JSON.stringify({ state: {
      tokens: null, user, cacheScope: scope, isAuthenticated: true,
      originalSession: null, isImpersonating: false,
    }, version: 0 }))
  }, { user: admin, scope: cacheScope })
  await context.route("**/api/v1/**", route => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/refresh") {
      const status = refreshStatus()
      return json(route, status === 200 ? tokens : { message: "Try again later" }, status)
    }
    return json(route, commonAPIResponse(path, admin) ?? { items: [] })
  })
  await context.route("**/portal/v1/**", route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith("/merchants")) return json(route, { items: [{ id: ids.merchant, name: "Test merchant", code: "TEST", role: 1, networkAccess: "allow" }] })
    if (path.endsWith("/balances")) return json(route, unvaluedPortalBalance([]))
    return json(route, { items: [], total: 0 })
  })
}

test("admin bootstrap recovers from 429 without returning to login or persisting access tokens", async ({ context, page }) => {
  let calls = 0
  await setup(context, () => ++calls === 1 ? 429 : 200)
  await page.goto("/portal")
  await expect(page.getByRole("heading", { name: "Обзор", exact: true })).toBeVisible()
  expect(calls).toBe(2)
  await expect(page).toHaveURL("/portal")
  expect(await page.evaluate(() => JSON.parse(localStorage.getItem("metapus-auth")!).state.tokens)).toBeNull()
})

test("temporary refresh outage shows recovery and allows an explicit retry in both layouts", async ({ context, page }) => {
  let unavailable = true
  await setup(context, () => unavailable ? 503 : 200)
  for (const path of ["/", "/portal"]) {
    unavailable = true
    await page.goto(path)
    await expect(page.getByRole("alert").filter({ hasText: "Не удалось восстановить сессию" })).toBeVisible()
    await expect(page).toHaveURL(path)
    unavailable = false
    await page.getByRole("button", { name: "Повторить попытку", exact: true }).click()
    await expect(page.getByRole("heading", { name: path === "/portal" ? "Обзор" : "Обзор платформы", exact: true })).toBeVisible()
  }
})

test("six admin tabs can recover within the shared auth rate limit", async ({ context, page }) => {
  let budget = 3 // Login and MFA also consume the shared burst of five.
  // Start refilling after the first 429 so slow startup cannot bypass the limit.
  let last: number | null = null
  let rejected = 0
  let accepted = 0
  await setup(context, () => {
    const now = Date.now()
    if (last !== null) {
      budget = Math.min(5, budget + (now - last) / 1000)
      last = now
    }
    if (budget < 1) { last ??= now; rejected++; return 429 }
    budget--; accepted++; return 200
  })
  const pages = [page, ...await Promise.all(Array.from({ length: 5 }, () => context.newPage()))]
  await Promise.all(pages.map(tab => tab.goto("/portal")))
  await Promise.all(pages.map(tab => expect(tab.getByRole("heading", { name: "Обзор", exact: true })).toBeVisible({ timeout: 15000 })))
  expect(rejected).toBeGreaterThan(0)
  expect(accepted).toBe(6)
  for (const tab of pages) await expect(tab).toHaveURL("/portal")
})

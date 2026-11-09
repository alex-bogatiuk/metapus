import { expect, test } from "@playwright/test"
import type { JsonObject } from "../lib/json-value"
import { commonAPIResponse, erpUser, ids, installSession, json, tokenRecord, tokenRequiredFields } from "./helpers"

const admin = { ...erpUser, isAdmin: true }

test("catalog list mounts and keeps the latest debounced search result", async ({ page }) => {
  await installSession(page, admin)
  let oldRequestStarted = false
  let releaseOld = () => { }
  const oldGate = new Promise<void>(resolve => { releaseOld = resolve })
  const metadata = {
    key: "token", name: "Token", type: "catalog", routePrefix: "tokens",
    presentation: { singular: "Токен", plural: "Токены" },
    fields: [...tokenRequiredFields, { name: "name", label: "Наименование", type: "string" }],
  }
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url())
    if (url.pathname === "/api/v1/meta/entities") return json(route, [metadata])
    if (url.pathname.endsWith("/filters")) return json(route, [])
    if (url.pathname.startsWith("/api/v1/meta/")) return json(route, metadata)
    const common = commonAPIResponse(url.pathname, admin)
    if (common !== undefined) return json(route, common)
    if (url.pathname === "/api/v1/catalog/tokens") {
      const search = url.searchParams.get("search")
      if (search === "old") { oldRequestStarted = true; await oldGate }
      return json(route, {
        items: [{ ...tokenRecord, name: search === "old" ? "Old token" : search === "new" ? "New token" : "Initial token" }],
        totalCount: 1, hasMore: false, hasPrev: false,
      })
    }
    return json(route, { items: [], unreadCount: 0 })
  })
  try {
    await page.goto("/catalogs/tokens")
    await expect(page.getByText("Initial token", { exact: true })).toBeVisible()
    const search = page.getByPlaceholder(/Поиск/)
    await search.fill("old")
    await expect.poll(() => oldRequestStarted).toBe(true)
    await search.fill("new")
    await expect(page.getByText("New token", { exact: true })).toBeVisible()
    const oldResponse = page.waitForResponse(response => new URL(response.url()).searchParams.get("search") === "old")
    releaseOld()
    await (await oldResponse).finished()
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())))
    await expect(page.getByText("New token", { exact: true })).toBeVisible()
    await expect(page.getByText("Old token", { exact: true })).toHaveCount(0)
  } finally { releaseOld() }
})

test("worker filters keep the latest result when an older response arrives last", async ({ page }) => {
  await installSession(page, admin)
  let initialCalls = 0
  let oldRequestStarted = false
  let releaseOld = () => { }
  const oldGate = new Promise<void>(resolve => { releaseOld = resolve })
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url())
    const common = commonAPIResponse(url.pathname, admin)
    if (common !== undefined) return json(route, common)
    if (url.pathname.endsWith("/worker-jobs/stats")) return json(route, { total: 1, success: 1, error: 0, avgDuration: 5 })
    if (url.pathname.endsWith("/worker-jobs")) {
      const status = url.searchParams.get("status")
      if (!status) initialCalls++
      if (status === "error") { oldRequestStarted = true; await oldGate }
      return json(route, {
        items: [{
          id: ids.token, jobName: status === "error" ? "stale-result" : status === "success" ? "latest-result" : "initial-result",
          jobCategory: "crypto", status: status || "success", startedAt: "2026-10-03T10:00:00Z",
          finishedAt: null, durationMs: null, itemsProcessed: null
        }],
        hasMore: false, totalCount: 1,
      })
    }
    return json(route, { items: [], unreadCount: 0 })
  })
  try {
    await page.goto("/admin/worker-jobs")
    await expect(page.getByText("initial-result", { exact: true })).toBeVisible()
    // Development StrictMode may replay mounting effects once.
    expect(initialCalls).toBeLessThanOrEqual(2)
    await page.getByRole("combobox").nth(2).click()
    await page.getByRole("option", { name: "Ошибка", exact: true }).click()
    await expect.poll(() => oldRequestStarted).toBe(true)
    await page.getByRole("combobox").nth(2).click()
    await page.getByRole("option", { name: "Успешно", exact: true }).click()
    await expect(page.getByText("latest-result", { exact: true })).toBeVisible()
    const oldResponse = page.waitForResponse(response => new URL(response.url()).searchParams.get("status") === "error")
    releaseOld()
    await (await oldResponse).finished()
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())))
    await expect(page.getByText("latest-result", { exact: true })).toBeVisible()
    await expect(page.getByText("stale-result", { exact: true })).toHaveCount(0)
  } finally { releaseOld() }
})

test("treasury token selection ignores balances returned for the previous token", async ({ page }) => {
  await installSession(page, admin)
  const secondToken = "10000000-0000-4000-8000-000000000023"
  let oldRequestStarted = false
  let releaseOld = () => { }
  const oldGate = new Promise<void>(resolve => { releaseOld = resolve })
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url())
    const common = commonAPIResponse(url.pathname, admin)
    if (common !== undefined) return json(route, common)
    if (url.pathname.endsWith("/catalog/tokens")) return json(route, {
      items: [{ ...tokenRecord, symbol: "FIRST", name: "First", decimalPlaces: 6 },
      { ...tokenRecord, id: secondToken, symbol: "SECOND", name: "Second", decimalPlaces: 6 }],
      totalCount: 2, hasMore: false, hasPrev: false,
    })
    if (url.pathname.endsWith("/availability")) {
      const tokenId = url.searchParams.get("tokenId")
      if (tokenId === ids.token) { oldRequestStarted = true; await oldGate }
      const amount = tokenId === ids.token ? "11000000" : "22000000"
      return json(route, { tokenId, amount, reserved: "0", available: amount })
    }
    return json(route, { items: [], totalCount: 0, hasMore: false, hasPrev: false, unreadCount: 0 })
  })
  try {
    await page.goto("/documents/treasury-withdrawals")
    await expect.poll(() => oldRequestStarted).toBe(true)
    await page.getByRole("combobox").nth(1).click()
    await page.getByRole("option", { name: "SECOND · Second", exact: true }).click()
    await expect(page.getByText("22 SECOND", { exact: true })).toBeVisible()
    const oldResponse = page.waitForResponse(response => new URL(response.url()).pathname.endsWith("/availability") && new URL(response.url()).searchParams.get("tokenId") === ids.token)
    releaseOld()
    await (await oldResponse).finished()
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())))
    await expect(page.getByText("22 SECOND", { exact: true })).toBeVisible()
    await expect(page.getByText("11 SECOND", { exact: true })).toHaveCount(0)
  } finally { releaseOld() }
})

test("security profile edits survive section switches and save the same contract", async ({ page }) => {
  await installSession(page, admin)
  let saved: JsonObject | undefined
  const profile = {
    id: ids.wallet, code: "PROFILE", name: "Original profile", description: "Original description", isSystem: false,
    createdAt: "2026-10-03T10:00:00Z", updatedAt: "2026-10-03T10:00:00Z", userCount: 0,
    dimensions: {}, fieldPolicies: [], policyRules: [],
  }
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path === `/api/v1/security/profiles/${ids.wallet}`) {
      if (route.request().method() === "PUT") saved = route.request().postDataJSON() as JsonObject
      return json(route, { ...profile, ...saved })
    }
    return json(route, { items: [], unreadCount: 0 })
  })
  await page.goto(`/admin/security-profiles/${ids.wallet}`)
  await page.getByRole("textbox").nth(1).fill("Updated profile")
  await page.getByRole("tab", { name: /Доступ к данным/ }).click()
  await page.getByRole("tab", { name: "Основное", exact: true }).click()
  await expect(page.getByRole("textbox").nth(1)).toHaveValue("Updated profile")
  await page.getByRole("button", { name: "Сохранить", exact: true }).click()
  await expect.poll(() => saved?.name).toBe("Updated profile")
  expect(saved).toMatchObject({ code: "PROFILE", description: "Original description", dimensions: {}, fieldPolicies: [] })
})

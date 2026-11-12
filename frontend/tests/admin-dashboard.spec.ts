import { test, expect, type Page, type Route } from "@playwright/test"
import { commonAPIResponse, erpUser, ids, installSession, json, unvaluedPortalBalance } from "./helpers"
import type { AdminDashboardFinanceResponse, AdminDashboardAnalyticsResponse, AdminDashboardQueuesResponse, CryptoNetworkStateResponse } from "../types/generated/http-contracts"
import type { JsonValue } from "../lib/json-value"

const admin = { ...erpUser, isAdmin: true }
const secondToken = "10000000-0000-4000-8000-000000000023"
const observedAt = "2026-10-03T12:00:00Z"
const assets = [ids.token, secondToken].map((tokenId, i) => ({ tokenId, networkId: ids.network, symbol: "USDT", network: i ? "TRON Nile" : "Ethereum Sepolia", decimalPlaces: 6, merchantMinor: "900719925474099312345678901", walletMinor: "100000000", reconciledLedgerMinor: null, chainMinor: null, deltaMinor: null, walletCount: 2, checkedWalletCount: 1, mismatchedWalletCount: 0, reconciledAt: null }))
const balance = unvaluedPortalBalance(assets)
const finance: AdminDashboardFinanceResponse = { assets, obligations: balance, wallets: balance, observedAt }
const analytics: AdminDashboardAnalyticsResponse = { period: "30d", fromDay: "2026-09-04", toDay: "2026-10-03", observedAt,
  assets: assets.map(a => ({ tokenId: a.tokenId, symbol: a.symbol, network: a.network, decimalPlaces: 6, feesMinor: "1234567", created: 61, paid: 50, underpaid: 2, expired: 3, pending: 2, cancelled: 1, awaitingConfirmation: 2, reversed: 1 })),
  receipts: assets.map((a, i) => ({ tokenId: a.tokenId, symbol: a.symbol, network: a.network, decimalPlaces: 6, day: "2026-10-03", deposits: i ? "2000000" : "1234567" })),
}
const queues: AdminDashboardQueuesResponse = { total: 61, withdrawals: 61, inbound: 0, outflows: 0, observedAt, items: [
  { id: ids.outflow, kind: "withdrawal", number: "WR-61", status: "pending_approval", merchant: "Acme", tokenId: ids.token, symbol: "USDT", network: "TRON Nile", decimalPlaces: 6, amount: "1234567", createdAt: observedAt },
] }
async function setup(page: Page, intercept?: (route: Route, url: URL) => Promise<boolean>, isAdmin = true) {
  const user = isAdmin ? admin : erpUser
  await installSession(page, user)
  let layout: JsonValue = { version: 21, widgets: [{ instanceId: "legacy", widgetType: "kpi", x: 0, y: 0, w: 3, h: 1, config: { metric: "cash-balance" } }] }
  const calls: Record<string, number> = {}
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url())
    calls[url.pathname] = (calls[url.pathname] ?? 0) + 1
    if (await intercept?.(route, url)) return
    if (url.pathname.endsWith("/me/preferences/dashboard-layout")) { layout = route.request().postDataJSON(); return route.fulfill({ status: 204 }) }
    if (url.pathname.endsWith("/me/preferences")) return json(route, { ...commonAPIResponse(url.pathname, user), dashboardLayout: layout })
    if (url.pathname.endsWith("/dashboard/queues")) return json(route, queues)
    if (url.pathname.endsWith("/dashboard/finance")) return json(route, finance)
    if (url.pathname.endsWith("/dashboard/analytics")) return json(route, { ...analytics, period: url.searchParams.get("period") })
    if (url.pathname.endsWith("/admin/crypto/networks")) return json(route, { items: [] })
    if (url.pathname.endsWith("/worker-jobs/stats")) return json(route, { total: 21, success: 19, error: 2, avgDuration: 12 })
    return json(route, commonAPIResponse(url.pathname, user) ?? { items: [] })
  })
  return { calls, saved: () => layout }
}
const widget = (page: Page, type: string) => page.locator(`[data-widget-type="${type}"]`)

test("legacy catalog migrates to 8 new types with exact balances and honest missing reconciliation", async ({ page }) => {
  await page.setViewportSize({ width: 1680, height: 2200 })
  const errors: string[] = []; page.on("pageerror", e => errors.push(e.message))
  await setup(page)
  await page.goto("/")
  await expect(page.locator("[data-widget-type]")).toHaveCount(12)
  await expect(widget(page, "attention-queue")).toContainText("Всего: 61")
  await expect(widget(page, "attention-queue")).toContainText("1,234567 USDT")
  await expect(widget(page, "attention-queue").getByRole("link", { name: "WR-61" })).toHaveAttribute("href", `/documents/withdrawal-request/${ids.outflow}`)
  await expect(widget(page, "financial-summary").first()).toContainText("900 719 925 474 099 312 345,678901".replaceAll(" ", " "))
  await expect(widget(page, "financial-summary").first()).toContainText("Нет оценки")
  await expect(widget(page, "custody-reconciliation")).toContainText("Нет полной сверки")
  await expect(page.getByText("Текущие дела", { exact: true })).toHaveCount(0)
  await expect(widget(page, "processing-health")).toContainText("Запуски: 21 · Ошибки: 2")
  await page.screenshot({ path: "test-results/admin-dashboard-desktop.png", fullPage: true })
  expect(errors).toEqual([])
})

test("gallery, per-instance settings, deletion and server persistence remain functional", async ({ page }) => {
  await page.setViewportSize({ width: 1680, height: 1100 })
  const mock = await setup(page)
  await page.goto("/")
  await expect(page.locator("[data-widget-type]")).toHaveCount(12)
  await page.getByRole("button", { name: "Настроить", exact: true }).click()
  await widget(page, "attention-queue").getByRole("button", { name: "Настроить: Требуют решения", exact: true }).click()
  await page.getByRole("spinbutton", { name: "Количество строк" }).fill("3")
  await page.getByRole("button", { name: "Применить", exact: true }).click()
  await page.getByRole("button", { name: "Добавить виджет", exact: true }).click()
  await page.getByRole("dialog").getByRole("button", { name: "Объём поступлений", exact: true }).click()
  await page.getByRole("button", { name: "Добавить на страницу", exact: true }).click()
  await expect(widget(page, "receipts-volume")).toHaveCount(2)
  await widget(page, "attention-counter").first().getByRole("button", { name: "Удалить: Требует внимания", exact: true }).click()
  await expect(page.locator("[data-widget-type]")).toHaveCount(12)
  await page.getByRole("button", { name: "Сохранить раскладку", exact: true }).click()
  await expect(page.getByRole("button", { name: "Настроить", exact: true })).toBeVisible()
  await page.reload()
  await expect(widget(page, "receipts-volume")).toHaveCount(2)
  await expect(widget(page, "attention-counter")).toHaveCount(3)
  const saved = mock.saved() as { catalogVersion: number; widgets: { widgetType: string; config: { limit?: number }; y: number }[] }
  expect(saved.catalogVersion).toBe(2)
  expect(saved.widgets.find(w => w.widgetType === "attention-queue")?.config.limit).toBe(3)
  expect(saved.widgets.every(w => Number.isFinite(w.y))).toBeTruthy()
})

test("analytics handles all periods, same-symbol networks and exact chart tooltips", async ({ page }) => {
  const periods: string[] = []
  await setup(page, async (_route, url) => { if (url.pathname.endsWith("/dashboard/analytics")) periods.push(url.searchParams.get("period")!); return false })
  await page.goto("/")
  const receipts = widget(page, "receipts-volume")
  await expect(receipts).toContainText("1,234567")
  for (const period of ["7 д", "90 д", "30 д"]) await receipts.getByRole("radio", { name: period, exact: true }).click()
  expect(periods).toEqual(expect.arrayContaining(["7d", "30d", "90d"]))
  await receipts.getByRole("combobox", { name: "Токен и сеть" }).click()
  await page.getByRole("option", { name: "USDT · TRON Nile", exact: true }).click()
  await expect(receipts).toContainText("2 USDT")
  await receipts.locator(".recharts-bar-rectangle path").last().hover()
  await expect(receipts.locator(".recharts-tooltip-wrapper")).toContainText("2 USDT")
  await expect(widget(page, "payment-results")).toContainText("На подтверждении")
})

test("global refresh revalidates all shared data, request failure has retry and never becomes zero", async ({ page }) => {
  const errors: string[] = []; page.on("pageerror", e => errors.push(e.message))
  let fail = true
  const mock = await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/dashboard/analytics") || !fail) return false
    await json(route, { code: "INTERNAL_ERROR", message: "Unavailable" }, 500); return true
  })
  await page.goto("/")
  await expect(widget(page, "receipts-volume").getByRole("alert")).toBeVisible()
  await expect(widget(page, "receipts-volume").getByText("Поступлений за период нет")).toHaveCount(0)
  await page.getByRole("button", { name: "Обновить", exact: true }).click()
  await expect(page.getByRole("button", { name: "Обновить", exact: true })).toBeEnabled()
  expect(errors).toEqual([])
  fail = false
  await widget(page, "receipts-volume").getByRole("button", { name: "Повторить" }).click()
  await expect(widget(page, "receipts-volume")).toContainText("1,234567")
  const before = mock.calls["/api/v1/admin/dashboard/finance"]
  await page.getByRole("button", { name: "Обновить", exact: true }).click()
  await expect.poll(() => mock.calls["/api/v1/admin/dashboard/finance"]).toBeGreaterThan(before)
})

test("failed layout save is visible and editing ends only after successful retry", async ({ page }) => {
  let fail = true
  await setup(page, async (route, url) => { if (!url.pathname.endsWith("/dashboard-layout") || !fail) return false; await json(route, { code: "INTERNAL_ERROR", message: "Unavailable" }, 500); return true })
  await page.goto("/")
  await page.getByRole("button", { name: "Настроить", exact: true }).click()
  await page.getByRole("button", { name: "Сохранить раскладку", exact: true }).click()
  await expect(page.getByRole("alert").filter({ hasText: "Раскладка не сохранена" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Сохранить раскладку", exact: true })).toBeVisible()
  fail = false
  await page.getByRole("button", { name: "Сохранить раскладку", exact: true }).click()
  await expect(page.getByRole("button", { name: "Настроить", exact: true })).toBeVisible()
})

test("non-admin cannot load tenant-wide widget data", async ({ page }) => {
  const mock = await setup(page, undefined, false)
  await page.goto("/")
  await expect(page.getByText("Обзор доступен администратору системы")).toBeVisible()
  expect(Object.keys(mock.calls).filter(p => p.includes("/admin/dashboard/"))).toEqual([])
})

test("small screens contain charts and tables without page overflow", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setup(page)
  await page.goto("/")
  await expect(widget(page, "receipts-volume")).toContainText("1,234567")
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy()
  await page.screenshot({ path: "test-results/admin-dashboard-mobile.png", fullPage: true })
})


test("dragging and resizing persist desktop positions without responsive rewrites", async ({ page }) => {
  await page.setViewportSize({ width: 1680, height: 1100 })
  const mock = await setup(page)
  await page.goto("/")
  await expect(widget(page, "attention-counter")).toHaveCount(4)
  await page.getByRole("button", { name: "Настроить", exact: true }).click()
  const counter = widget(page, "attention-counter").first()
  const handle = await counter.locator(".drag-handle").boundingBox()
  expect(handle).not.toBeNull()
  await page.mouse.move(handle!.x + 30, handle!.y + 10)
  await page.mouse.down()
  await page.mouse.move(handle!.x + 380, handle!.y + 10, { steps: 12 })
  await page.mouse.up()
  const resize = await counter.locator(".react-resizable-handle").count()
  // RGL attaches the resize handle to the grid item outside the Card.
  expect(resize).toBe(0)
  const corner = await counter.locator("..").locator(".react-resizable-handle").boundingBox()
  expect(corner).not.toBeNull()
  await page.mouse.move(corner!.x + 5, corner!.y + 5)
  await page.mouse.down()
  await page.mouse.move(corner!.x + 130, corner!.y + 5, { steps: 12 })
  await page.mouse.up()
  await page.getByRole("button", { name: "Сохранить раскладку", exact: true }).click()
  await expect(page.getByRole("button", { name: "Настроить", exact: true })).toBeVisible()
  const saved = mock.saved() as { widgets: { instanceId: string; x: number; y: number; w: number }[] }
  const moved = saved.widgets.find(w => w.instanceId === "default-withdrawals")!
  expect(moved.x !== 0 || moved.y !== 0).toBeTruthy()
  expect(moved.w).toBeGreaterThan(3)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.reload()
  await expect(widget(page, "attention-counter")).toHaveCount(4)
  expect(mock.saved()).toEqual(saved)
})

test("network health uses the readiness rule: a degraded reserve RPC warns, a role without healthy RPC is a problem", async ({ page }) => {
  await page.setViewportSize({ width: 1680, height: 1100 })
  const now = new Date().toISOString()
  const policy = (networkId: string) => ({ networkId, enabled: true, perTransactionMaxFeeMinor: "1", dailyNetworkFeeBudgetMinor: "1", feeFunding: { mode: "source_only", dailyBudgetMinor: "0", minFeeWalletBalanceMinor: "0", minSourceWalletResidualMinor: "0", maxFundingAmountMinor: "0" }, retryDelaySeconds: 15, rpcFailureThreshold: 3, rpcOpenSeconds: 30, version: 1, updatedBy: "admin", createdAt: now, updatedAt: now })
  const endpoint = (role: string, providerId: string, state: string) => ({ endpointId: `${providerId}-${role}`, role, providerId, state, failures: state === "closed" ? 0 : 1 })
  const sepolia: CryptoNetworkStateResponse = {
    network: { id: ids.network, code: "ETHEREUM-SEPOLIA", family: "evm", networkIdentifier: "11155111", nativeTokenId: ids.token, nativeTokenSymbol: "ETH", nativeDecimals: 18, requiredConfirmations: 12, finalityMode: "confirmations" },
    watching: true,
    capabilities: { family: "evm", walletProvisioning: "derived", requiredRpcRoles: ["rpc"], externalResourceMode: "none", sequenceModel: "account_nonce", checkpointTimeUnit: "unix_seconds", tokenAccountModel: "same_address", minWatcherLagSeconds: 60, supportsReplacement: true },
    policy: policy(ids.network),
    runtime: { networkId: ids.network, signerReady: true, resourceProviderReady: false, resourceProviderState: "unconfigured", observedAt: now, operationalHold: false,
      rpcEndpoints: [endpoint("rpc", "alchemy", "closed"), endpoint("rpc", "infura", "closed"), endpoint("rpc", "publicnode", "degraded"), endpoint("rpc", "zeroxrpc", "closed")] },
  }
  const nile: CryptoNetworkStateResponse = {
    ...sepolia,
    network: { ...sepolia.network, id: secondToken, code: "TRON-NILE", family: "tron", networkIdentifier: "nile" },
    capabilities: { ...sepolia.capabilities, family: "tron", requiredRpcRoles: ["fullnode", "solidity"] },
    policy: policy(secondToken),
    runtime: { ...sepolia.runtime, networkId: secondToken, rpcEndpoints: [endpoint("fullnode", "trongrid", "closed"), endpoint("solidity", "trongrid", "open")] },
  }
  await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/admin/crypto/networks")) return false
    await json(route, { items: [sepolia, nile] })
    return true
  })
  await page.goto("/")
  const health = widget(page, "processing-health")
  await expect(health.getByText("RPC нестабилен: 1 из 4", { exact: true })).toBeVisible()
  await expect(health.getByText("Нет готового RPC: solidity", { exact: true })).toBeVisible()
  await expect(health.getByText("Проблема с RPC", { exact: true })).toHaveCount(0)
  const counter = widget(page, "attention-counter").filter({ hasText: "Сети с проблемами" })
  await expect(counter).toContainText("1")
})

import { test, expect, type Page, type Route } from "@playwright/test"
import { commonAPIResponse, ids, installPortalSession, json, portalOwner } from "./helpers"
import type { PortalBalanceResponse, PortalChartPoint } from "../configuration/crypto/types/portal-api"
import type { PortalRiskBalanceResponse } from "../types/generated/http-contracts"

const secondMerchant = "10000000-0000-4000-8000-000000000021"
const ethToken = "10000000-0000-4000-8000-000000000022"
const tronUsdt = "10000000-0000-4000-8000-000000000023"
const now = new Date("2026-10-02T12:00:00Z")
const bucket = (amount: string) => ({ amount, coverage: "complete" as const, unvaluedCount: 0 })
const balance: PortalBalanceResponse = {
  baseCurrency: { id: ids.network, code: "USD", name: "US Dollar", decimalPlaces: 2 },
  source: { id: ids.address, name: "CoinGecko", code: "COINGECKO", sourceType: "coingecko" },
  evaluatedAt: now.toISOString(), freshUntil: "2026-10-02T12:10:00Z",
  valuation: { total: bucket("462.22"), available: bucket("434.75"), pending: bucket("27.47") },
  byToken: [
    { tokenId: ethToken, tokenSymbol: "ETH", networkId: ids.network, network: "Ethereum Sepolia", decimalPlaces: 18, currencyCode: "ETH", rawAmount: "160000000000000000", humanAmount: "0.16", availableRaw: "150000000000000000", availableHuman: "0.15", pendingRaw: "10000000000000000", pendingHuman: "0.01", valuation: { status: "fresh", reason: "selected", selectedQuote: null, lastKnownQuote: null, freshUntil: "2026-10-02T12:10:00Z", total: "439.45", available: "411.98", pending: "27.47" } },
    { tokenId: ids.token, tokenSymbol: "USDT", networkId: ids.network, network: "Ethereum Sepolia", decimalPlaces: 6, currencyCode: "USDT", rawAmount: "14500000", humanAmount: "14.5", availableRaw: "14500000", availableHuman: "14.5", pendingRaw: "0", pendingHuman: "0", valuation: { status: "fresh", reason: "selected", selectedQuote: null, lastKnownQuote: null, freshUntil: "2026-10-02T12:10:00Z", total: "14.5", available: "14.5", pending: "0" } },
    { tokenId: tronUsdt, tokenSymbol: "USDT", networkId: ids.address, network: "TRON Nile Testnet", decimalPlaces: 6, currencyCode: "USDT", rawAmount: "8270000", humanAmount: "8.27", availableRaw: "8270000", availableHuman: "8.27", pendingRaw: "0", pendingHuman: "0", valuation: { status: "fresh", reason: "selected", selectedQuote: null, lastKnownQuote: null, freshUntil: "2026-10-02T12:10:00Z", total: "8.27", available: "8.27", pending: "0" } },
  ],
}
const receipts: PortalChartPoint[] = [
  { day: "2026-10-01", deposits: "10000000000000000", tokenId: ethToken, symbol: "ETH", network: "Ethereum Sepolia", decimalPlaces: 18 },
  { day: "2026-10-02", deposits: "20000000000000000", tokenId: ethToken, symbol: "ETH", network: "Ethereum Sepolia", decimalPlaces: 18 },
  { day: "2026-10-02", deposits: "1234567", tokenId: ids.token, symbol: "USDT", network: "Ethereum Sepolia", decimalPlaces: 6 },
  { day: "2026-10-02", deposits: "2000000", tokenId: tronUsdt, symbol: "USDT", network: "TRON Nile Testnet", decimalPlaces: 6 },
]
const invoices = ["completed", "underpaid", "partially_paid", "paid", "expired"].map((status, index) => ({
  id: "10000000-0000-4000-8000-" + String(30 + index).padStart(12, "0"),
  number: "CI-2026-" + (14 - index), status, amount: "1000000", receivedAmount: index === 1 ? "500000" : "1000000",
  symbol: "USDT", network: "Ethereum Sepolia", decimalPlaces: 6, createdAt: now.toISOString(), paymentCount: 1, processingFee: "0", netAmount: "1000000",
}))

async function setup(page: Page, intercept?: (route: Route, url: URL) => Promise<boolean>, role = 1) {
  await page.clock.install({ time: now })
  await installPortalSession(page)
  await page.route("**/api/v1/**", route => json(route, commonAPIResponse(new URL(route.request().url()).pathname, { ...portalOwner, accessScopes: { merchant: [ids.merchant, secondMerchant] } }) ?? { items: [] }))
  await page.route("**/portal/v1/**", async route => {
    const url = new URL(route.request().url())
    if (await intercept?.(route, url)) return
    const path = url.pathname
    if (path.endsWith("/merchants")) return json(route, { items: [{ id: ids.merchant, name: "Acme", code: "ACME", role, networkAccess: "allow" }, { id: secondMerchant, name: "Beta", code: "BETA", role, networkAccess: "allow" }] })
    if (path.endsWith("/balances/risk")) return json(route, { snapshotId: "empty", asOf: now.toISOString(), freshUntil: "2026-10-02T12:10:00Z", items: [] })
    if (path.endsWith("/balances") || path.endsWith("/dashboard/balance")) return json(route, url.searchParams.get("merchant_id") === secondMerchant ? { ...balance, valuation: { total: bucket("900"), available: bucket("900"), pending: bucket("0") } } : balance)
    if (path.endsWith("/dashboard/chart")) return json(route, { items: receipts })
    if (path.endsWith("/dashboard/funnel")) return json(route, { total: 14, receivedAny: 14, fullyPaid: 12, confirmed: 12, expired: 2 })
    if (path.endsWith("/invoices")) return json(route, { items: invoices, total: 14 })
    if (path.endsWith("/withdrawals")) return json(route, { items: [{ id: ids.outflow, number: "CW-42", status: "broadcast", amount: "1000000", symbol: "USDT", network: "TRON Nile Testnet", decimalPlaces: 6, destAddress: "TDestination", createdAt: now.toISOString() }], total: 1 })
    return json(route, { items: [] })
  })
}

const riskSnapshot: PortalRiskBalanceResponse = {
  snapshotId: "risk-1", asOf: now.toISOString(), freshUntil: "2026-10-02T12:10:00Z",
  items: [{ merchantId: ids.merchant, tokenId: ids.token, networkId: ids.network, symbol: "USDT", network: "TRON Nile Testnet", decimalPlaces: 6,
    routingEnabled: true, scopeVersion: 1, allocationComplete: true, reason: "source_allocation_complete", hasHighExposure: true,
    ledgerBalance: "90000000", pendingChain: "1000000", pendingRestoration: "0", total: "90000000", free: "90000000", reserved: "0", technicalHold: "0",
    buckets: [
      { riskClass: "low", total: "70000000", free: "70000000", reserved: "0", technicalHold: "0" },
      { riskClass: "medium", total: "10000000", free: "10000000", reserved: "0", technicalHold: "0" },
      { riskClass: "high", total: "8000000", free: "8000000", reserved: "0", technicalHold: "0" },
      { riskClass: "unknown", total: "2000000", free: "2000000", reserved: "0", technicalHold: "0" },
    ],
  }],
}

test("risk balance keeps adverse warnings visible and saves the optional breakdown per user", async ({ page }) => {
  await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/balances/risk")) return false
    await json(route, riskSnapshot)
    return true
  })
  let expanded = false
  await page.route("**/api/v1/me/preferences**", async route => {
    if (route.request().method() === "PUT") {
      expanded = route.request().postDataJSON().expandedSections?.portalRiskBreakdown === true
      return route.fulfill({ status: 204 })
    }
    return json(route, { userId: ids.user, interface: { expandedSections: { portalRiskBreakdown: expanded } }, listFilters: {}, listColumns: {}, dashboardLayout: null, favorites: [], updatedAt: now.toISOString() })
  })
  await page.goto("/portal")
  const card = page.getByTestId("risk-balance")
  await expect(card.getByText(/Высокий риск: 8,00/)).toBeVisible()
  await expect(card.getByText(/Риск не определён: 2,00/)).toBeVisible()
  await expect(card.getByRole("table")).toHaveCount(0)
  await card.getByRole("button", { name: "По риску" }).click()
  await expect(card.getByRole("table")).toBeVisible()
  await expect(card.getByRole("row").filter({ hasText: "Низкий" })).toContainText("70,00")
  await expect.poll(() => expanded).toBe(true)
  await page.reload()
  await expect(card.getByRole("table")).toBeVisible()
  await page.setViewportSize({ width: 1440, height: 1250 })
  await card.scrollIntoViewIfNeeded()
  await page.screenshot({ path: "test-results/portal-risk-balance-desktop.png", fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await card.scrollIntoViewIfNeeded()
  await expect(card.getByText(/Высокий риск: 8,00/)).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: "test-results/portal-risk-balance-mobile.png", fullPage: true })
})

test("risk balance hides expired classes when refresh fails", async ({ page }) => {
  let fail = false
  await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/balances/risk")) return false
    if (fail) await route.abort("failed")
    else await json(route, { ...riskSnapshot, freshUntil: "2026-10-02T12:00:30Z" })
    return true
  })
  await page.goto("/portal")
  const card = page.getByTestId("risk-balance")
  await card.getByRole("button", { name: "По риску" }).click()
  await expect(card.getByRole("table")).toBeVisible()
  fail = true
  await page.clock.fastForward(31000)
  await expect(card.getByText(/Снимок риска устарел/)).toBeVisible()
  await expect(card.getByRole("table")).toHaveCount(0)
  await expect(card.getByText(/В последнем снимке были источники с высоким риском/)).toBeVisible()
})

test("overview consolidates balance, networks and real operation actions", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1250 })
  const errors: string[] = []
  page.on("pageerror", error => errors.push(error.message))
  await setup(page)
  await page.goto("/portal")
  await expect(page).toHaveURL("/portal")
  await expect(page.getByTestId("overview-total")).toContainText("462,22 USD")
  await expect(page.getByRole("link", { name: "Баланс", exact: true })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Создать заявку на вывод" })).toHaveAttribute("href", "/portal/withdrawals/new")
  await expect(page.getByRole("link", { name: "Создать инвойс", exact: true })).toHaveAttribute("href", "/portal/invoices/new")
  await expect(page.getByText("Завершён", { exact: true })).toBeVisible()
  await expect(page.getByText("Недоплата", { exact: true })).toBeVisible()
  await expect(page.getByText("Оплачен частично", { exact: true })).toBeVisible()
  await expect(page.getByRole("row").filter({ hasText: "ETH" }).first()).toContainText("0,15")
  await page.getByRole("button", { name: "Как рассчитан баланс" }).click()
  await expect(page.getByRole("dialog")).toContainText("Ethereum Sepolia")
  await expect(page.getByRole("dialog")).toContainText("TRON Nile Testnet")
  await page.keyboard.press("Escape")
  await page.getByRole("radio", { name: "Выводы", exact: true }).click()
  await expect(page.getByText("Отправлен в сеть", { exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "Все операции" })).toHaveAttribute("href", "/portal/withdrawals")
  await page.getByTestId("overview-total").scrollIntoViewIfNeeded()
  await page.screenshot({ path: "test-results/portal-overview-desktop.png", fullPage: true })
  expect(errors).toEqual([])
})

test("analytics renders actual token units, tooltip, all periods and separate networks", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1350 })
  const periods: string[] = []
  await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/dashboard/chart")) return false
    periods.push(url.searchParams.get("period")!)
    await json(route, { items: receipts })
    return true
  })
  await page.goto("/portal")
  await expect(page.getByTestId("overview-total")).toBeVisible()
  expect(periods).toEqual([])
  await page.getByRole("tab", { name: "Аналитика" }).click()
  await expect(page.getByTestId("overview-total")).toBeVisible()
  await expect(page.getByTestId("receipts-total")).toContainText("0,03 ETH")
  await expect(page.locator(".recharts-bar-rectangle path")).toHaveCount(2)
  await expect(page.getByRole("heading", { name: "Оплата инвойсов" })).toBeVisible()
  await expect(page.getByText("85,7% от созданных")).toHaveCount(2)
  await expect(page.getByText(/Срок оплаты истёк: 2 \(14,3%\)/)).toBeVisible()
  await page.locator(".recharts-bar-rectangle path").last().hover()
  await expect(page.locator(".recharts-tooltip-wrapper")).toContainText("0,02 ETH")
  for (const days of [7, 90, 30]) {
    await page.getByRole("radio", { name: days + " дней", exact: true }).click()
    await expect(page.getByRole("table", { name: "Поступления по дням" }).getByRole("row")).toHaveCount(days + 1)
  }
  expect(periods).toEqual(expect.arrayContaining(["7d", "30d", "90d"]))
  await page.getByLabel("Актив и сеть").click()
  await page.getByRole("option", { name: "USDT · Ethereum Sepolia", exact: true }).click()
  await expect(page.getByTestId("receipts-total")).toContainText("1,234567 USDT")
  await page.getByLabel("Актив и сеть").click()
  await page.getByRole("option", { name: "USDT · TRON Nile Testnet", exact: true }).click()
  await expect(page.getByTestId("receipts-total")).toContainText("2 USDT")
  await page.mouse.move(200, 50)
  await page.getByTestId("overview-total").scrollIntoViewIfNeeded()
  await page.screenshot({ path: "test-results/portal-analytics-desktop.png", fullPage: true })
})

test("analytics distinguishes failed requests, retries and zero receipts", async ({ page }) => {
  let chartCalls = 0
  await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/dashboard/chart")) return false
    chartCalls++
    await json(route, chartCalls === 1 ? { code: "INTERNAL_ERROR", message: "Unavailable" } : { items: [] }, chartCalls === 1 ? 500 : 200)
    return true
  })
  await page.goto("/portal")
  await page.getByRole("tab", { name: "Аналитика" }).click()
  await expect(page.getByRole("alert").filter({ hasText: "Не удалось загрузить поступления" })).toBeVisible()
  await expect(page.getByText("За этот период поступлений нет", { exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Повторить загрузку" }).click()
  await expect(page.getByText("За этот период поступлений нет", { exact: true })).toBeVisible()
  await expect(page.getByTestId("receipts-total")).toContainText("0 ETH")
  await expect(page.locator(".recharts-bar-rectangle")).toHaveCount(0)
})

test("balance failure can be retried without blocking analytics", async ({ page }) => {
  let balanceCalls = 0
  await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/balances")) return false
    balanceCalls++
    await json(route, balanceCalls === 1 ? { code: "INTERNAL_ERROR", message: "Unavailable" } : balance, balanceCalls === 1 ? 500 : 200)
    return true
  })
  await page.goto("/portal")
  await expect(page.getByRole("alert").filter({ hasText: "Не удалось загрузить баланс" })).toBeVisible()
  await page.getByRole("tab", { name: "Аналитика" }).click()
  await expect(page.getByTestId("receipts-total")).toContainText("0,03 ETH")
  await page.getByRole("button", { name: "Повторить загрузку" }).click()
  await expect(page.getByTestId("overview-total")).toContainText("462,22")
})

test("mobile and dark layouts contain wide tables; viewer has read-only actions", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setup(page, undefined, 3)
  await page.goto("/portal")
  await expect(page.getByTestId("overview-total")).toBeVisible()
  await expect(page.getByRole("link", { name: "Создать заявку на вывод" })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Создать инвойс", exact: true })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole("tab", { name: "Аналитика" }).click()
  await expect(page.getByTestId("receipts-total")).toContainText("0,03")
  await page.evaluate(() => document.documentElement.classList.add("dark"))
  await page.clock.runFor(300)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.setViewportSize({ width: 390, height: 2400 })
  await page.getByTestId("overview-total").scrollIntoViewIfNeeded()
  await page.screenshot({ path: "test-results/portal-analytics-mobile-dark.png", fullPage: true })
})

test("merchant switch discards delayed analytics", async ({ page }) => {
  let release: (() => void) | undefined
  await setup(page, async (route, url) => {
    if (!url.pathname.endsWith("/dashboard/chart")) return false
    if (url.searchParams.get("merchant_id") === ids.merchant) await new Promise<void>(resolve => { release = resolve })
    await json(route, { items: url.searchParams.get("merchant_id") === secondMerchant ? [{ ...receipts[0], deposits: "50000000000000000" }] : receipts })
    return true
  })
  await page.goto("/portal")
  await expect(page.getByTestId("overview-total")).toBeVisible()
  await page.getByRole("tab", { name: "Аналитика" }).click()
  await expect.poll(() => !!release).toBe(true)
  await page.getByRole("button", { name: /Acme/ }).click()
  await page.getByRole("menuitem", { name: /Beta/ }).click()
  await expect(page.getByTestId("overview-total")).toContainText("900,00")
  release!()
  await page.getByRole("tab", { name: "Аналитика" }).click()
  await expect(page.getByTestId("receipts-total")).toContainText("0,05 ETH")
  await expect(page.getByTestId("receipts-total")).not.toContainText("0,03")
})

test("invoice analytics retries failures and empty states remain explicit", async ({ page }) => {
  let funnelCalls = 0
  await setup(page, async (route, url) => {
    if (url.pathname.endsWith("/balances")) {
      await json(route, { ...balance, byToken: [], valuation: { total: bucket("0"), available: bucket("0"), pending: bucket("0") } })
      return true
    }
    if (url.pathname.endsWith("/invoices")) {
      await json(route, { items: [], total: 0 })
      return true
    }
    if (url.pathname.endsWith("/dashboard/funnel")) {
      funnelCalls++
      await json(route, funnelCalls === 1 ? { code: "INTERNAL_ERROR", message: "Unavailable" } : { total: 0, receivedAny: 0, fullyPaid: 0, confirmed: 0, expired: 0 }, funnelCalls === 1 ? 500 : 200)
      return true
    }
    return false
  })
  await page.goto("/portal")
  await expect(page.getByTestId("overview-total")).toContainText("0,00 USD")
  await expect(page.getByText("На балансе пока нет средств")).toBeVisible()
  await expect(page.getByText("Инвойсов пока нет", { exact: true })).toBeVisible()
  await page.getByRole("tab", { name: "Средства и операции" }).focus()
  await page.keyboard.press("ArrowRight")
  await expect(page.getByRole("tab", { name: "Аналитика" })).toHaveAttribute("data-state", "active")
  await expect(page.getByRole("alert").filter({ hasText: "Не удалось загрузить аналитику инвойсов" })).toBeVisible()
  await page.getByRole("button", { name: "Повторить загрузку" }).click()
  await expect(page.getByText("Инвойсов пока нет", { exact: true })).toBeVisible()
})

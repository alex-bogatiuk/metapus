import type { JsonObject } from "@/lib/json-value"
import { expect, test } from "@playwright/test"

import { commonAPIResponse, ids, installPortalSession, json, portalOwner, portalRoles } from "./helpers"

const currency = {
  tokenId: ids.token,
  symbol: "USDT",
  networkId: ids.network,
  network: "Sepolia",
  count: 1,
  totalMinor: "100000000",
  invoiceSharePct: "100",
  decimalPlaces: 6,
}

const merchantResponse = {
  items: [{
    id: ids.merchant,
    name: "Acme",
    code: "ACME",
    role: portalRoles.owner,
    networkAccess: "allow" as const,
  }],
}

test("invoice retry keeps one idempotency key and one business payload", async ({ page }) => {
  await installPortalSession(page)
  const keys: string[] = []
  const bodies: JsonObject[] = []

  await page.route("**/api/v1/**", async (route) => {
    const pathname = new URL(route.request().url()).pathname
    return json(route, commonAPIResponse(pathname, portalOwner) ?? { items: [] })
  })
  await page.route("**/portal/v1/**", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === "/portal/v1/merchants") return json(route, merchantResponse)
    if (pathname === "/portal/v1/dashboard/currencies") return json(route, { items: [currency] })
    if (pathname === "/portal/v1/invoices" && request.method() === "POST") {
      keys.push(request.headers()["idempotency-key"] ?? "")
      bodies.push(request.postDataJSON() as JsonObject)
      if (keys.length === 1) {
        return json(route, { code: "TEMPORARY_UNAVAILABLE", message: "temporary command failure" }, 503)
      }
      return json(route, {
        object: "invoice", version: 1,
        id: ids.outflow,
        checkoutRef: "ci_0123456789abcdef0123456789abcdef",
        payUrl: "https://pay.example.test/pay/i/ci_0123456789abcdef0123456789abcdef",
        number: "INV-42",
        status: "created",
        amount: "12500000",
        symbol: "USDT",
        network: "Sepolia",
        decimalPlaces: 6,
        walletAddress: "0x1111111111111111111111111111111111111111",
        expiresAt: "2026-08-30T13:00:00Z",
        createdAt: "2026-08-30T12:00:00Z",
      })
    }
    return json(route, { items: [] })
  })

  await page.goto("/portal/invoices/new")
  await page.getByLabel("Сеть").click()
  await page.getByRole("option", { name: /Sepolia/i }).click()
  await page.getByLabel("Токен").click()
  await page.getByRole("option", { name: /USDT/i }).click()
  await page.getByLabel("Сумма").fill("12.5")
  await page.getByLabel("Order ID").fill("ORDER-42")
  await page.getByRole("button", { name: "Создать инвойс" }).click()
  await expect.poll(() => keys.length).toBe(1)

  await page.getByRole("button", { name: "Создать инвойс" }).click()
  await expect(page).toHaveURL(`/portal/invoices/${ids.outflow}`)

  expect(keys).toHaveLength(2)
  expect(keys[0]).toMatch(/^[0-9a-f-]{36}$/i)
  expect(keys[1]).toBe(keys[0])
  expect(bodies[0]).toEqual(bodies[1])
  expect(bodies[0]).toMatchObject({
    tokenId: ids.token,
    amount: "12.5",
    orderId: "ORDER-42",
  })
  // The default lifetime is resolved from merchant settings on the server.
  expect(bodies[0]).not.toHaveProperty("ttlMinutes")
})

test("withdrawal step-up retries the same durable command without changing balance input", async ({ page }) => {
  await installPortalSession(page)
  const keys: string[] = []
  const bodies: JsonObject[] = []
  let stepUpBody: JsonObject | undefined

  await page.route("**/api/v1/**", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === "/api/v1/auth/step-up") {
      stepUpBody = request.postDataJSON() as JsonObject
      return json(route, { expiresAt: "2026-08-30T12:05:00Z" })
    }
    return json(route, commonAPIResponse(pathname, portalOwner) ?? { items: [] })
  })
  await page.route("**/portal/v1/**", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === "/portal/v1/merchants") return json(route, merchantResponse)
    if (pathname === "/portal/v1/dashboard/currencies") return json(route, { items: [currency] })
    if (pathname === "/portal/v1/dashboard/balance") {
      return json(route, {
        baseCurrency: { id: ids.network, code: "USD", name: "US Dollar", decimalPlaces: 2 },
        source: null, evaluatedAt: "2026-08-30T12:00:00Z", freshUntil: null,
        valuation: {
          total: { amount: null, coverage: "none", unvaluedCount: 1 },
          pending: { amount: null, coverage: "none", unvaluedCount: 1 },
          available: { amount: null, coverage: "none", unvaluedCount: 1 },
        },
        byToken: [{
          tokenId: ids.token,
          tokenSymbol: "USDT",
          decimalPlaces: 6,
          currencyCode: "USD",
          rawAmount: "100000000",
          humanAmount: "100",
          pendingRaw: "0", pendingHuman: "0", availableRaw: "100000000", availableHuman: "100",
          valuation: { status: "unavailable", reason: "no_rate", selectedQuote: null, lastKnownQuote: null, freshUntil: null, total: null, pending: null, available: null },
        }],
      })
    }
    // Risk routing is off for this token, so the limit is the ledger balance and the request carries no risk class.
    if (pathname === "/portal/v1/balances/risk") {
      return json(route, { snapshotId: "risk-off", asOf: "2026-08-30T12:00:00Z", freshUntil: "2026-08-30T12:10:00Z", items: [{
        merchantId: ids.merchant, networkId: ids.network, tokenId: ids.token, symbol: "USDT", network: "Sepolia", decimalPlaces: 6,
        routingEnabled: false, scopeVersion: 1, allocationComplete: true, reason: "", hasHighExposure: false,
        ledgerBalance: "100000000", pendingChain: "0", pendingRestoration: "0", total: "100000000", free: "100000000", reserved: "0", technicalHold: "0",
        buckets: [],
      }] })
    }
    if (pathname === "/portal/v1/withdrawal-addresses") {
      return json(route, { items: [{
        id: ids.address,
        networkId: ids.network,
        network: "Sepolia",
        address: "0x2222222222222222222222222222222222222222",
        label: "Treasury",
        createdAt: "2026-08-30T10:00:00Z",
      }] })
    }
    if (pathname === "/portal/v1/withdrawal-requests" && request.method() === "POST") {
      keys.push(request.headers()["idempotency-key"] ?? "")
      bodies.push(request.postDataJSON() as JsonObject)
      if (keys.length === 1) {
        return json(route, {
          code: "STEP_UP_REQUIRED",
          message: "fresh step-up required",
          details: { actionCode: "portal.withdrawal_requests.create" },
        }, 403)
      }
      return json(route, { object: "withdrawal_request", version: 1, id: ids.adjustment, number: "WR-42", status: "pending_signature" })
    }
    if (pathname === "/portal/v1/withdrawal-requests") return json(route, { items: [], total: 0 })
    return json(route, { items: [] })
  })

  await page.goto("/portal/withdrawals/new")
  const selects = page.getByRole("combobox")
  await selects.nth(0).click()
  await page.getByRole("option", { name: /USDT/ }).click()
  await selects.nth(1).click()
  await page.getByRole("option", { name: /0x222222/ }).click()
  await expect(page.getByText(/Макс: 100 USDT/)).toBeVisible()
  await page.getByPlaceholder("0.00").fill("25")
  await page.getByRole("button", { name: "Отправить заявку" }).click()

  const dialog = page.getByRole("dialog")
  await expect(dialog.getByRole("heading", { name: "Подтверждение действия" })).toBeVisible()
  await dialog.getByLabel("Пароль").fill("correct horse battery staple")
  await dialog.getByLabel("Код TOTP").fill("123456")
  await dialog.getByRole("button", { name: "Подтвердить" }).click()

  await expect(page).toHaveURL("/portal/withdrawals/requests")
  expect(keys).toHaveLength(2)
  expect(keys[0]).toMatch(/^[0-9a-f-]{36}$/i)
  expect(keys[1]).toBe(keys[0])
  expect(bodies[0]).toEqual(bodies[1])
  expect(bodies[0]).toEqual({ tokenId: ids.token, amount: "25", addressId: ids.address })
  expect(stepUpBody).toEqual({
    actionCode: "portal.withdrawal_requests.create",
    password: "correct horse battery staple",
    code: "123456",
  })
})

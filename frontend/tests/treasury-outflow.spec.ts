import type { JsonObject } from "@/lib/json-value"
import { expect, test } from "@playwright/test"

import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const observedOutflow = {
  id: ids.outflow,
  networkId: ids.network,
  tokenId: ids.token,
  sourceWalletId: ids.wallet,
  txHash: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  fromAddress: "0x1111111111111111111111111111111111111111",
  toAddress: "0x2222222222222222222222222222222222222222",
  contractAddress: "0x3333333333333333333333333333333333333333",
  amount: "1250000",
  blockNumber: 42,
  blockHash: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  confirmations: 12,
  requiredConfirmations: 12,
  eventKind: "token_transfer",
  eventIndex: 3,
  finalityStatus: "final",
  status: "pending_review" as const,
  detectedAt: "2026-08-30T08:00:00Z",
}

const pendingDetail = {
  outflow: observedOutflow,
  classificationReason: "",
  classificationEvidence: {},
  adjustment: null,
  matchedBasisType: "",
  matchedBasisId: "",
  openHolds: [{
    scope: "wallet_allocation",
    reason: "external_outflow",
    ownerType: "external_outflow",
    ownerId: ids.outflow,
    heldAt: "2026-08-30T08:00:00Z",
  }],
  actions: [],
  resolution: "",
  canResolve: false,
  resolutionBlockers: ["classification is required"],
}

test("external outflow classification sends a durable command and exposes custody workflow", async ({ page }) => {
  await installSession(page)
  const classificationKeys: string[] = []
  let classificationBody: JsonObject | undefined
  let stepUpBody: JsonObject | undefined

  await page.route("**/api/v1/**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const common = commonAPIResponse(url.pathname)
    if (common !== undefined) return json(route, common)

    if (url.pathname === "/api/v1/auth/step-up") {
      stepUpBody = request.postDataJSON() as JsonObject
      return json(route, { expiresAt: "2026-08-30T08:10:00Z" })
    }

    if (url.pathname === "/api/v1/catalog/tokens") {
      return json(route, { items: [{
        id: ids.token, code: "USDT_SEPOLIA", name: "USDT Sepolia", symbol: "USDT",
        networkId: ids.network, decimalPlaces: 6, tokenStandard: "erc20", deletionMark: false,
        contractAddress: "0x0000000000000000000000000000000000000001", isActive: true,
        sweepThreshold: "0", sweepMaxAgeHours: 0, sweepRetainAmount: "0", isFolder: false, version: 1,
      }], totalCount: 1, hasMore: false, hasPrev: false })
    }
    if (url.pathname === "/api/v1/document/treasury-withdrawal/addresses") {
      return json(route, { items: [{ id: ids.address, networkId: ids.network, address: observedOutflow.toAddress, label: "Custody", isActive: true }] })
    }
    if (url.pathname === "/api/v1/document/treasury-withdrawal") return json(route, { items: [], totalCount: 0, hasMore: false, hasPrev: false })
    if (url.pathname === "/api/v1/document/treasury-withdrawal/reconciliation/outflows") {
      return json(route, { items: [observedOutflow] })
    }
    if (url.pathname === `/api/v1/document/treasury-withdrawal/reconciliation/outflows/${ids.outflow}`) {
      return json(route, pendingDetail)
    }
    if (url.pathname.endsWith(`/reconciliation/outflows/${ids.outflow}/classify`)) {
      classificationKeys.push(request.headers()["idempotency-key"] ?? "")
      classificationBody = request.postDataJSON() as JsonObject
      if (classificationKeys.length === 1) {
        return json(route, {
          code: "STEP_UP_REQUIRED",
          message: "fresh object-bound step-up required",
          details: { actionCode: `crypto.reconciliation.classify:${ids.outflow}` },
        }, 403)
      }
      return json(route, {
        ...pendingDetail,
        outflow: { ...observedOutflow, status: "classified", classification: "unauthorized_loss" },
        classificationReason: "confirmed by custody investigation",
        adjustment: {
          id: ids.adjustment,
          externalOutflowId: ids.outflow,
          networkId: ids.network,
          walletId: ids.wallet,
          tokenId: ids.token,
          amount: "1250000",
          status: "pending_approval",
          makerId: ids.user,
          postedVersion: 0,
          createdAt: "2026-08-30T08:05:00Z",
        },
        actions: [{
          actorType: "user", actorId: ids.user, action: "classified",
          oldState: "pending_review", newState: "classified",
          reason: "confirmed by custody investigation", createdAt: "2026-08-30T08:05:00Z",
        }],
        resolutionBlockers: ["custody adjustment must be approved and posted"],
      })
    }
    if (url.pathname.endsWith("/availability")) {
      return json(route, { tokenId: ids.token, amount: "10000000", reserved: "0", available: "10000000" })
    }
    if (url.pathname.endsWith("/source-wallets")) return json(route, { items: [] })
    return json(route, { items: [] })
  })

  await page.goto("/documents/treasury-withdrawals")
  await page.getByRole("tab", { name: /Сверка \(1\)/ }).click()
  await page.getByText("pending_review", { exact: true }).click()

  const dialog = page.getByRole("dialog")
  await expect(dialog.getByRole("heading", { name: "Внешнее списание" })).toBeVisible()
  await expect(dialog.getByText("token_transfer #3")).toBeVisible()
  await expect(dialog.getByText(/Открытые operational holds: 1/)).toBeVisible()

  await dialog.locator("textarea").first().fill("confirmed by custody investigation")
  await dialog.getByRole("button", { name: "Сохранить классификацию" }).click()

  const stepUp = page.getByRole("dialog").filter({ hasText: "Подтверждение действия" })
  await expect(stepUp).toBeVisible()
  await stepUp.getByLabel("Пароль").fill("correct horse battery staple")
  await stepUp.getByLabel("Код TOTP").fill("123456")
  await stepUp.getByRole("button", { name: "Подтвердить" }).click()

  await expect(dialog.getByRole("heading", { name: "CryptoCustodyAdjustment" })).toBeVisible()
  await expect(dialog.getByText("pending_approval", { exact: true })).toBeVisible()
  expect(classificationKeys).toHaveLength(2)
  expect(classificationKeys[0]).toMatch(/^[0-9a-f-]{36}$/i)
  expect(classificationKeys[1]).toBe(classificationKeys[0])
  expect(classificationBody).toMatchObject({
    classification: "unauthorized_loss",
    reason: "confirmed by custody investigation",
  })
  expect(stepUpBody).toEqual({
    actionCode: `crypto.reconciliation.classify:${ids.outflow}`,
    password: "correct horse battery staple",
    code: "123456",
  })
})

test("dashboard deep link opens the reconciliation tab and the specific outflow", async ({ page }) => {
  await installSession(page, { ...erpUser, isAdmin: true })
  const writes: string[] = []
  await page.route("**/api/v1/**", async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() !== "GET" && path.includes("/reconciliation/")) writes.push(request.method() + " " + path)
    const common = commonAPIResponse(path, { ...erpUser, isAdmin: true })
    if (common !== undefined) return json(route, common)
    if (path === `/api/v1/document/treasury-withdrawal/reconciliation/outflows/${ids.outflow}`) return json(route, pendingDetail)
    if (path === "/api/v1/document/treasury-withdrawal") return json(route, { items: [], totalCount: 0, hasMore: false, hasPrev: false })
    if (path === "/api/v1/catalog/tokens") return json(route, { items: [], totalCount: 0, hasMore: false, hasPrev: false })
    return json(route, { items: [] })
  })
  await page.goto(`/documents/treasury-withdrawals?tab=reconciliation&outflow=${ids.outflow}`)
  await expect(page.getByRole("dialog").getByRole("heading", { name: "Внешнее списание", exact: true })).toBeVisible()
  await expect(page.getByRole("dialog")).toContainText(observedOutflow.txHash)
  await page.keyboard.press("Escape")
  await expect(page.getByRole("tab", { name: /Сверка/ })).toHaveAttribute("data-state", "active")
  expect(writes).toEqual([])
})

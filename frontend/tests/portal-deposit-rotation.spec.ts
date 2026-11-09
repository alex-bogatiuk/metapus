import { test, expect, type Page } from "@playwright/test"
import { commonAPIResponse, ids, installPortalSession, json, portalOwner } from "./helpers"
import type { PortalDepositAddressItem, PortalRotateDepositAddressResponse } from "../types/portal-api"

const newWallet = "10000000-0000-4000-8000-000000000099"
const newAddress = "TNewCustomerDepositAddressForRotation"

function address(customerId: string, used: boolean): PortalDepositAddressItem {
  return {
    id: used ? ids.wallet : ids.address, customerId, address: used ? "TOldCustomerDepositAddress" : "TUnusedDepositAddress",
    networkId: ids.network, networkCode: "TRON-NILE", networkName: "TRON Nile", tokens: ["USDT"],
    topUpCount: used ? 1 : 0, createdAt: "2026-10-28T10:00:00Z", riskClass: used ? "high" : "unknown",
    riskEnabled: true, riskAssets: used ? [{ tokenId: ids.token, symbol: "USDT", hasFunds: true, inventoryCovered: true,
      assessment: { riskClass: "high", enabled: true, source: "inventory", reason: "known_high_exposure", evaluatedAt: "2026-10-29T10:00:00Z" } }] : [],
    canRotate: used, rotationBlockedReason: used ? "" : "unused_address",
  }
}

async function setup(page: Page, role = 2) {
  await installPortalSession(page)
  let rotated = false
  let requests = 0
  let idempotencyKey: string | undefined
  await page.route("**/api/v1/**", route => json(route, commonAPIResponse(new URL(route.request().url()).pathname, portalOwner) ?? { items: [] }))
  await page.route("**/portal/v1/**", async route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith("/merchants")) return json(route, { items: [{ id: ids.merchant, name: "Acme", code: "ACME", role, networkAccess: "allow" }] })
    if (url.pathname.endsWith("/rotate")) {
      requests++
      idempotencyKey = route.request().headers()["idempotency-key"]
      expect(route.request().postDataJSON()).toEqual({ reason: "aml_high_risk" })
      rotated = true
      const response: PortalRotateDepositAddressResponse = {
        object: "deposit_address", id: newWallet, customerId: "high-customer", address: newAddress,
        networkId: ids.network, networkCode: "TRON-NILE", networkName: "TRON Nile", tokens: ["USDT"], reused: false,
        createdAt: "2026-10-29T11:00:00Z", rotationId: ids.adjustment, oldWalletId: ids.wallet,
        oldAddress: "TOldCustomerDepositAddress", resourceVersion: 1, nextRotationAt: "2026-10-30T11:00:00Z",
        qrCode: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
      }
      return json(route, response)
    }
    if (url.pathname.endsWith("/deposit-addresses")) return json(route, {
      items: [address("unused-customer", false), rotated ? { ...address("high-customer", true), id: newWallet, address: newAddress,
        topUpCount: 0, canRotate: false, riskClass: "unknown", riskAssets: [], rotationBlockedReason: "unused_address" } : address("high-customer", true)],
      total: 2, quotas: [{ networkId: ids.network, networkCode: "TRON-NILE", networkName: "TRON Nile", used: 2, max: 10, tokens: ["USDT"] }],
    })
    return json(route, { items: [] })
  })
  await page.goto("/portal/deposit-addresses")
  await expect(page.getByRole("row").filter({ hasText: "high-customer" })).toBeVisible()
  return { count: () => requests, key: () => idempotencyKey }
}

test("manager sees AML evidence, cannot replace unused address, and receives new requisites and QR after rotation", async ({ page }) => {
  const observed = await setup(page)
  const unused = page.getByRole("row").filter({ hasText: "unused-customer" })
  await expect(unused.getByRole("button", { name: "Заменить", exact: true })).toBeDisabled()
  await page.screenshot({ path: "test-results/deposit-address-rotation-list.png", fullPage: true })
  const high = page.getByRole("row").filter({ hasText: "high-customer" })
  await expect(high.getByRole("button", { name: "Заменить", exact: true })).toBeInViewport({ ratio: 1 })
  await high.getByRole("button", { name: "AML-риск адреса high-customer" }).click()
  await expect(page.getByRole("dialog").getByText("Обнаружена связь поступлений с источником высокого риска")).toBeVisible()
  await page.keyboard.press("Escape")
  await high.getByRole("button", { name: "Заменить", exact: true }).click()
  await expect(page.getByRole("dialog").getByText(/не будут автоматически зачислены/)).toBeVisible()
  await page.getByRole("button", { name: "Подтвердить замену" }).click()
  await expect(page.getByRole("dialog").getByText(newAddress, { exact: true })).toBeVisible()
  await expect(page.getByRole("img", { name: "QR-код депозитного адреса high-customer" })).toBeVisible()
  await page.screenshot({ path: "test-results/deposit-address-rotation-result.png", fullPage: true })
  expect(observed.count()).toBe(1)
  expect(observed.key()).toBeTruthy()
})

test("viewer can inspect AML evidence and has no rotation action", async ({ page }) => {
  await setup(page, 3)
  await expect(page.getByRole("button", { name: "Заменить", exact: true })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "AML-риск адреса high-customer" })).toBeVisible()
})

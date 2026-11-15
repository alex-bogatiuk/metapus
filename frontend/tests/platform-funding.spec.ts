import { expect, test, type Page } from "@playwright/test"
import type { PreparePlatformFundingRequest, UnidentifiedReceiptItem } from "../types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const receiptId = "20000000-0000-4000-8000-000000000001"
const fundingId = "20000000-0000-4000-8000-000000000002"
const base: UnidentifiedReceiptItem = {
  receiptId, inboxEventId: "", number: "CCR-2026-00009", receivedAt: "2026-11-11T17:17:40Z",
  networkId: ids.network, networkName: "TRON Nile", tokenId: ids.token, tokenSymbol: "TRX", decimalPlaces: 6,
  walletId: ids.wallet, toAddress: "TYEFSrRc9bR157gSNQC98zTSFpk4MEtvT3", fromAddress: "TJexternalPayer0000000000000000000",
  txHash: "64e6b9fa9a07637164a1b2b1fcdf8fc67bb1495adbb059982a76d03c690cac56", amount: "1000000000", remainder: "1000000000",
  reason: "no_allocation", state: "unattributed", custodyMerchantId: "", invoiceId: "", pendingError: "", credit: null,
  paymentId: "", paymentNumber: "", paymentStatus: "", platformFunding: null,
  merchantTie: { merchantId: ids.merchant, merchantName: "CryptoGate", link: "invoice_allocation" }, refund: null,
}

async function setup(page: Page, item: UnidentifiedReceiptItem) {
  await installSession(page)
  const writes: { path: string; body: PreparePlatformFundingRequest | { version: number }; key: string | undefined }[] = []
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_receipt:read", "crypto_receipt:credit", "crypto_receipt:approve"] })
    const common = commonAPIResponse(path, erpUser)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/crypto/unidentified-receipts") return json(route, { items: [item], total: 1 })
    if (route.request().method() === "POST" && path.startsWith("/api/v1/crypto/")) {
      writes.push({ path, body: route.request().postDataJSON(), key: route.request().headers()["idempotency-key"] })
      return json(route, { id: fundingId, number: "CPF-2026-00001", receiptId, status: "prepared", version: 1, amount: item.amount,
        reason: "funding", merchantLinkAcknowledged: true })
    }
    return json(route, { items: [] })
  })
  await page.goto("/crypto/unidentified-receipts")
  await expect(page.getByText(item.number, { exact: true })).toBeVisible()
  return { writes }
}

test("a merchant-tied receipt becomes platform funding only with a reason and an explicit acknowledgement", async ({ page }) => {
  const state = await setup(page, base)
  await expect(page.getByText("Связан с мерчантом «CryptoGate»", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Пополнение платформы", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByText("Кошелёк связан с мерчантом «CryptoGate»", { exact: true })).toBeVisible()
  await expect(dialog.getByText(/адрес был выдан под счёт мерчанта/)).toBeVisible()
  const submit = dialog.getByRole("button", { name: "Отнести к пополнению", exact: true })
  await expect(submit).toBeDisabled()
  await dialog.getByLabel("Основание", { exact: true }).fill("Собственный перевод из казначейства")
  await expect(submit).toBeDisabled()
  await dialog.getByRole("checkbox").check()
  await submit.click()
  await expect(page.getByRole("dialog")).toHaveCount(0)
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].path).toBe(`/api/v1/crypto/unidentified-receipts/${receiptId}/platform-fundings`)
  expect(state.writes[0].body).toEqual({ reason: "Собственный перевод из казначейства", acknowledgeMerchantTie: true })
  expect(state.writes[0].key).toBeTruthy()
})

test("the approving operator sees the merchant tie before posting a prepared platform funding", async ({ page }) => {
  const state = await setup(page, {
    ...base, state: "awaiting_approval",
    platformFunding: { id: fundingId, number: "CPF-2026-00001", status: "prepared", version: 3, reason: "Собственный перевод",
      merchantName: "CryptoGate", merchantLink: "invoice_allocation", preparedBy: "30000000-0000-4000-8000-000000000001",
      decidedAt: null, decisionReason: "" },
  })
  await expect(page.getByRole("button", { name: "Пополнение платформы", exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByText("Кошелёк связан с мерчантом «CryptoGate»", { exact: true })).toBeVisible()
  await expect(dialog.getByText(/Основание: Собственный перевод/)).toBeVisible()
  await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click()
  await expect(page.getByRole("dialog")).toHaveCount(0)
  expect(state.writes.map(write => [write.path, write.body])).toEqual([[`/api/v1/crypto/platform-fundings/${fundingId}/approve`, { version: 3 }]])
})

test("the preparing operator cannot approve their own platform funding", async ({ page }) => {
  await setup(page, {
    ...base, merchantTie: null, state: "awaiting_approval",
    platformFunding: { id: fundingId, number: "CPF-2026-00001", status: "prepared", version: 1, reason: "Собственный перевод",
      merchantName: "", merchantLink: "", preparedBy: ids.user, decidedAt: null, decisionReason: "" },
  })
  await expect(page.getByText("Ждёт другого сотрудника", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Подтвердить", exact: true })).toHaveCount(0)
})

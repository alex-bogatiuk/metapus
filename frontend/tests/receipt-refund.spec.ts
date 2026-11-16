import { expect, test, type Page } from "@playwright/test"
import type { CreateReceiptRefundRequest, UnidentifiedReceiptItem } from "../configuration/crypto/types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const receiptId = "20000000-0000-4000-8000-000000000011"
const refundId = "20000000-0000-4000-8000-000000000012"
const hotWallet = "30000000-0000-4000-8000-000000000031"
const sender = "TJexternalPayer0000000000000000000"
const payer = "TPayerConfirmedAddress000000000000"
const receipt: UnidentifiedReceiptItem = {
  receiptId, inboxEventId: "", number: "CCR-2026-00021", receivedAt: "2026-11-11T17:17:40Z",
  networkId: ids.network, networkName: "TRON Nile", tokenId: ids.token, tokenSymbol: "USDT", decimalPlaces: 6,
  walletId: ids.wallet, toAddress: "TYEFSrRc9bR157gSNQC98zTSFpk4MEtvT3", fromAddress: sender,
  txHash: "64e6b9fa9a07637164a1b2b1fcdf8fc67bb1495adbb059982a76d03c690cac57", amount: "25000000", remainder: "25000000",
  reason: "no_allocation", state: "unattributed", custodyMerchantId: "", invoiceId: "", pendingError: "", credit: null,
  paymentId: "", paymentNumber: "", paymentStatus: "", platformFunding: null, merchantTie: null, refund: null,
}

async function setup(page: Page, item: UnidentifiedReceiptItem, permissions: string[]) {
  await installSession(page)
  const writes: { path: string; body: CreateReceiptRefundRequest; key: string | undefined }[] = []
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_receipt:read", ...permissions] })
    const common = commonAPIResponse(path, erpUser)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/crypto/unidentified-receipts") return json(route, { items: [item], total: 1 })
    if (path === "/api/v1/document/treasury-withdrawal/source-wallets") {
      return json(route, { items: [{ id: hotWallet, networkId: ids.network, name: "Treasury hot",
        address: "THotWallet000000000000000000000000", ledgerAmount: "500000000", reservedAmount: "0", availableAmount: "500000000" }] })
    }
    if (path === "/api/v1/document/treasury-withdrawal/receipt-refunds" && route.request().method() === "POST") {
      writes.push({ path, body: route.request().postDataJSON(), key: route.request().headers()["idempotency-key"] })
      return json(route, { object: "treasury_withdrawal", id: refundId, version: 1, number: "TW-2026-00007", date: "2026-11-15T10:00:00Z",
        tokenId: ids.token, sourceWalletId: hotWallet, addressId: "30000000-0000-4000-8000-000000000032", destAddress: payer,
        amount: "23500000", networkFee: "0", status: "pending_signature", description: "Ошибочный перевод без счёта",
        createdAt: "2026-11-15T10:00:00Z", updatedAt: "2026-11-15T10:00:00Z",
        purpose: "receipt_refund", receiptId, gasPayer: "recipient", withheldAmount: "1500000" }, 202)
    }
    return json(route, { items: [] })
  })
  await page.goto("/crypto/unidentified-receipts")
  await expect(page.getByText(item.number, { exact: true })).toBeVisible()
  return { writes }
}

test("a refund needs a confirmed destination, evidence and a reason and can withhold the gas", async ({ page }) => {
  const state = await setup(page, receipt, ["document:treasury_withdrawal:create"])
  await page.getByRole("button", { name: "Вернуть отправителю", exact: true }).click()
  const dialog = page.getByRole("dialog")
  // The sender is only a hint: it may be an exchange address.
  await expect(dialog.getByText(sender)).toBeVisible()
  await expect(dialog.getByLabel("Адрес получателя")).toHaveValue("")
  await expect(dialog.getByLabel(/Сумма из невыясненных/)).toHaveValue("25")
  const submit = dialog.getByRole("button", { name: "Создать возврат", exact: true })
  await expect(submit).toBeDisabled()

  await dialog.getByLabel("Адрес получателя").fill(payer)
  await dialog.getByLabel("Подтверждение адреса").fill("Плательщик подписал сообщение этим адресом")
  await dialog.getByLabel("Причина возврата").fill("Ошибочный перевод без счёта")
  await dialog.getByLabel("Кошелёк-источник").selectOption(hotWallet)
  await dialog.getByLabel(/Сумма из невыясненных/).fill("26")
  await expect(dialog.getByRole("alert")).toContainText("Сумма больше остатка")
  await expect(submit).toBeDisabled()
  await dialog.getByLabel(/Сумма из невыясненных/).fill("25")

  await dialog.getByRole("radio", { name: "Получатель" }).click()
  await expect(submit).toBeDisabled()
  await dialog.getByLabel(/Удержать на комиссию/).fill("1.5")
  await expect(dialog.getByText("Получатель получит:")).toContainText("23.5 USDT")
  await submit.click()
  await expect(page.getByRole("dialog")).toHaveCount(0)

  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].key).toBeTruthy()
  expect(state.writes[0].body).toEqual({
    receiptId, sourceWalletId: hotWallet, destAddress: payer,
    evidence: "Плательщик подписал сообщение этим адресом", reason: "Ошибочный перевод без счёта",
    amount: "25000000", gasPayer: "recipient", withheldAmount: "1500000",
  })
})

test("a pending refund blocks other decisions and points to treasury approval", async ({ page }) => {
  await setup(page, {
    ...receipt, state: "awaiting_approval",
    refund: { id: refundId, number: "TW-2026-00007", status: "pending_approval", amount: "23500000", withheld: "1500000", destination: payer, preparedBy: ids.user },
  }, ["document:treasury_withdrawal:create", "crypto_receipt:credit"])
  await expect(page.getByText(/TW-2026-00007 · Возврат отправителю/)).toContainText("ждёт утверждения в казначействе")
  await expect(page.getByRole("button", { name: "Вернуть отправителю", exact: true })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Зачислить мерчанту", exact: true })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Утвердить в казначействе" })).toHaveAttribute("href", "/documents/treasury-withdrawals")
})

test("without treasury rights the refund is not offered", async ({ page }) => {
  await setup(page, receipt, ["crypto_receipt:credit"])
  await expect(page.getByRole("button", { name: "Зачислить мерчанту", exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Вернуть отправителю", exact: true })).toHaveCount(0)
})

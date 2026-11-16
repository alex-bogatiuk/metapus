import { expect, test } from "@playwright/test"
import type { WalletFundingListResponse } from "../configuration/crypto/types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const list: WalletFundingListResponse = {
  total: 1,
  items: [{
    id: "20000000-0000-4000-8000-000000000010", networkName: "Ethereum Sepolia", walletId: ids.wallet, walletCode: "HOT-EVM-001",
    tokenId: ids.token, tokenSymbol: "ETH", decimalPlaces: 18, txHash: "0xbbee2d23ff33241414e1ea03092563feed8a1958cc40e13897c4cc5060280dd4",
    fromAddress: "0x1111111111111111111111111111111111111111", toAddress: "0x81708152089fEd8881FE2a71580C9b097A4D111b",
    amount: "50000000000000000", blockNumber: 12119276, confirmations: 22, blockTimestamp: "2026-11-12T16:00:00Z", recordedAt: "2026-11-12T16:20:00Z",
  }],
}

test("recorded wallet fundings are listed with their wallet, amount and block", async ({ page }) => {
  await installSession(page)
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_receipt:read"] })
    const common = commonAPIResponse(path, erpUser)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/crypto/wallet-fundings") return json(route, list)
    return json(route, { items: [] })
  })
  await page.goto("/crypto/wallet-fundings")
  await expect(page.getByRole("heading", { name: "Пополнения кошельков", exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: "HOT-EVM-001", exact: true })).toHaveAttribute("href", `/catalogs/wallets/${ids.wallet}`)
  await expect(page.getByText("0.05 ETH", { exact: true })).toBeVisible()
  await expect(page.getByText("22 подтв.", { exact: true })).toBeVisible()
  await expect(page.getByText("1 всего", { exact: true })).toBeVisible()
})

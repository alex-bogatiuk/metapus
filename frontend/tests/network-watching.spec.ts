import { expect, test, type Page } from "@playwright/test"
import type { CryptoNetworkStateResponse, SetCryptoNetworkWatchingRequest } from "../types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const admin = { ...erpUser, isAdmin: true }
const now = new Date().toISOString()
const network: CryptoNetworkStateResponse = {
  network: { id: ids.network, code: "ETHEREUM-SEPOLIA", family: "evm", networkIdentifier: "11155111", nativeTokenId: ids.token, nativeTokenSymbol: "ETH", nativeDecimals: 18, requiredConfirmations: 12, finalityMode: "confirmations" },
  watching: true,
  capabilities: { family: "evm", walletProvisioning: "derived", requiredRpcRoles: ["rpc"], externalResourceMode: "none", sequenceModel: "account_nonce", checkpointTimeUnit: "unix_seconds", tokenAccountModel: "same_address", minWatcherLagSeconds: 60, supportsReplacement: true },
  policy: { networkId: ids.network, enabled: false, perTransactionMaxFeeMinor: "3000000000000000", dailyNetworkFeeBudgetMinor: "50000000000000000", feeFunding: { mode: "source_only", dailyBudgetMinor: "0", minFeeWalletBalanceMinor: "0", minSourceWalletResidualMinor: "0", maxFundingAmountMinor: "0" }, retryDelaySeconds: 15, rpcFailureThreshold: 3, rpcOpenSeconds: 30, version: 1, updatedBy: "admin", createdAt: now, updatedAt: now },
  runtime: { networkId: ids.network, rpcEndpoints: [], signerReady: true, resourceProviderReady: false, resourceProviderState: "unconfigured", observedAt: now, operationalHold: false },
}

async function setup(page: Page, refuseFirst?: string) {
  await installSession(page, admin)
  let current = { ...network }
  const writes: { body: SetCryptoNetworkWatchingRequest; key: string | undefined }[] = []
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_network:read", "crypto_network:update"] })
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/admin/crypto/networks") return json(route, { items: [current] })
    if (path === `/api/v1/admin/crypto/networks/${ids.network}/watching` && route.request().method() === "POST") {
      const body: SetCryptoNetworkWatchingRequest = route.request().postDataJSON()
      writes.push({ body, key: route.request().headers()["idempotency-key"] })
      if (refuseFirst && writes.length === 1) return json(route, { code: "VALIDATION_ERROR", message: refuseFirst }, 400)
      current = { ...current, watching: body.watching }
      return json(route, current)
    }
    return json(route, { items: [] })
  })
  await page.goto("/admin/crypto-networks")
  await expect(page.getByText("Наблюдение за сетью", { exact: true })).toBeVisible()
  return { writes }
}

async function confirm(page: Page, reason: string) {
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByRole("button", { name: "Подтвердить", exact: true })).toBeDisabled()
  await dialog.getByLabel("Причина действия", { exact: true }).fill(reason)
  await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click()
}

test("stopping watching explains a guard refusal and then leaves processing unavailable", async ({ page }) => {
  const state = await setup(page, "network watching cannot stop while invoices await payment or finality")
  await page.getByRole("button", { name: "Остановить наблюдение", exact: true }).click()
  await confirm(page, "Стенд не используется")
  await expect(page.getByText("В сети есть счета, ожидающие оплаты или подтверждений. Дождитесь их завершения или истечения.", { exact: true })).toBeVisible()

  await page.getByRole("dialog").getByRole("button", { name: "Повторить запрос", exact: true }).click()
  await expect(page.getByRole("dialog")).toHaveCount(0)
  await expect(page.getByText("Сеть не читается", { exact: true }).first()).toBeVisible()
  await expect(page.getByRole("switch", { name: "Обработка операций", exact: true })).toBeDisabled()
  expect(state.writes.map(write => write.body)).toEqual([
    { watching: false, reason: "Стенд не используется" },
    { watching: false, reason: "Стенд не используется" },
  ])
  expect(state.writes[0].key).toBeTruthy()

  await page.getByRole("button", { name: "Возобновить наблюдение", exact: true }).click()
  await confirm(page, "Стенд снова нужен")
  await expect(page.getByText("Сеть читается", { exact: true })).toBeVisible()
  await expect(page.getByRole("switch", { name: "Обработка операций", exact: true })).toBeEnabled()
  expect(state.writes[2].body).toEqual({ watching: true, reason: "Стенд снова нужен" })
})

import { expect, test, type Page } from "@playwright/test"
import type { CryptoNetworkIngestionStatus, CryptoNetworkStateResponse } from "../types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const admin = { ...erpUser, isAdmin: true }
const now = new Date().toISOString()
const hash = "0000000004093dc1f2a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5a1f9"

function tronNetwork(ingestion?: CryptoNetworkIngestionStatus, watching = true): CryptoNetworkStateResponse {
  return {
    network: { id: ids.network, code: "TRON-NILE", family: "tron", networkIdentifier: "nile", nativeTokenId: ids.token, nativeTokenSymbol: "TRX", nativeDecimals: 6, requiredConfirmations: 19, finalityMode: "protocol_finalized" },
    watching,
    capabilities: { family: "tron", walletProvisioning: "derived_with_activation", requiredRpcRoles: ["rpc"], externalResourceMode: "none", sequenceModel: "none", checkpointTimeUnit: "unix_millis", tokenAccountModel: "same_address", minWatcherLagSeconds: 60, supportsReplacement: false },
    policy: { networkId: ids.network, enabled: false, perTransactionMaxFeeMinor: "30000000", dailyNetworkFeeBudgetMinor: "500000000", feeFunding: { mode: "source_only", dailyBudgetMinor: "0", minFeeWalletBalanceMinor: "0", minSourceWalletResidualMinor: "0", maxFundingAmountMinor: "0" }, retryDelaySeconds: 15, rpcFailureThreshold: 3, rpcOpenSeconds: 30, version: 1, updatedBy: "admin", createdAt: now, updatedAt: now },
    runtime: { networkId: ids.network, rpcEndpoints: [], signerReady: true, resourceProviderReady: false, resourceProviderState: "unconfigured", observedAt: now, operationalHold: false, ingestion },
  }
}

async function open(page: Page, network: CryptoNetworkStateResponse) {
  await installSession(page, admin)
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_network:read", "crypto_network:update"] })
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/admin/crypto/networks") return json(route, { items: [network] })
    return json(route, { items: [] })
  })
  await page.goto("/admin/crypto-networks")
  await expect(page.getByText("TRON-NILE", { exact: true })).toBeVisible()
}

test("the card header shows the worker's sync state and lag of the deciding stream", async ({ page }) => {
  await open(page, tronNetwork({
    state: "catching_up",
    streams: [
      { watcherKey: "tron:solidified", optional: false, state: "catching_up", reason: "backlog", lastBlock: 67891204, blockHash: hash, headHeight: 67891230, lagBlocks: 26, lagSeconds: 78, progressAgeSeconds: 2, leaseActive: true },
      { watcherKey: "tron:latest", optional: true, state: "degraded", reason: "ingestion RPC coverage unavailable", lagBlocks: -1, lagSeconds: -1, progressAgeSeconds: -1, leaseActive: false },
    ],
  }))

  const pill = page.getByLabel(/^Догоняет\./)
  await expect(pill).toContainText("#67 891 204")
  await expect(pill).toContainText("−26 бл.")
  await expect(pill).toHaveAttribute("aria-label", /голова 67\s891\s230 · отставание 26 бл\. · 1 мин 18 с · Догоняет по блокам/)

  const table = page.getByLabel("Стримы синхронизации блоков")
  const expand = page.getByRole("button", { name: "Развернуть сеть TRON-NILE", exact: true })
  if (await expand.isVisible()) await expand.click()
  await expect(table).toBeVisible()
  const solidified = table.getByRole("row", { name: /Подтверждённые блоки/ })
  await expect(solidified).toContainText("67 891 204")
  await expect(solidified).toContainText("00000000…a1f9")
  await expect(solidified).toContainText("Идёт")
  const latest = table.getByRole("row", { name: /Голова цепи \(JIT\)/ })
  await expect(latest).toContainText("необязательный")
  await expect(latest).toContainText("RPC не отдаёт голову стрима")
  await expect(latest).toContainText("Нет воркера")

  await page.context().grantPermissions(["clipboard-read", "clipboard-write"])
  await solidified.getByRole("button", { name: "Скопировать хеш блока", exact: true }).click()
  await expect(solidified.getByRole("button", { name: "Хеш скопирован", exact: true })).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(hash)
})

test("a network without a fresh worker report says so instead of showing a stale state", async ({ page }) => {
  await open(page, tronNetwork(undefined))
  await expect(page.getByText("Синхронизация: нет данных", { exact: true })).toBeVisible()
})

test("an unwatched network shows no sync pill", async ({ page }) => {
  await open(page, tronNetwork({ state: "ready", streams: [] }, false))
  await expect(page.getByText("Сеть не читается", { exact: true }).first()).toBeVisible()
  await expect(page.getByText("Синхронизирована", { exact: true })).toHaveCount(0)
  await expect(page.getByText("Синхронизация: нет данных", { exact: true })).toHaveCount(0)
})

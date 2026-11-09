import { expect, test, type Page } from "@playwright/test"
import type { CryptoNetworkStateResponse, CryptoShadowControlResponse, SetCryptoShadowControlRequest } from "../types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const admin = { ...erpUser, isAdmin: true }
const now = new Date().toISOString()
const network: CryptoNetworkStateResponse = {
  network: { id: ids.network, code: "TRON-NILE", family: "tron", networkIdentifier: "nile", nativeTokenId: ids.token, nativeTokenSymbol: "TRX", nativeDecimals: 6, requiredConfirmations: 20, finalityMode: "confirmations" },
  capabilities: { family: "tron", walletProvisioning: "derived_with_activation", requiredRpcRoles: ["fullnode", "solidity"], externalResourceMode: "optional", sequenceModel: "none", checkpointTimeUnit: "unix_millis", tokenAccountModel: "same_address", minWatcherLagSeconds: 180, supportsReplacement: false },
  policy: { networkId: ids.network, enabled: false, perTransactionMaxFeeMinor: "30000000", dailyNetworkFeeBudgetMinor: "300000000", feeFunding: { mode: "source_only", dailyBudgetMinor: "0", minFeeWalletBalanceMinor: "0", minSourceWalletResidualMinor: "0", maxFundingAmountMinor: "0" }, retryDelaySeconds: 30, rpcFailureThreshold: 3, rpcOpenSeconds: 30, version: 1, updatedBy: "admin", createdAt: now, updatedAt: now },
  runtime: { networkId: ids.network, rpcEndpoints: [], signerReady: true, resourceProviderReady: true, resourceProviderState: "ready", observedAt: now, operationalHold: false },
}
const initial: CryptoShadowControlResponse = {
  networkId: ids.network, family: "tron", mode: "shadow", activatedAt: null, paused: false, version: 1,
  leaseActive: true, checkpointHeight: 72011000, lastProgressAt: now, workerObservedAt: now,
  lastErrorCode: "", status: "running", canChange: true, lockedReason: "",
}

async function setup(page: Page, give = initial, administrator = true, failure?: "conflict" | "retry") {
  const user = administrator ? admin : erpUser
  await installSession(page, user)
  let current = { ...give }
  const writes: { body: SetCryptoShadowControlRequest; key: string | undefined }[] = []
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_network:read", "crypto_network:update"] })
    const common = commonAPIResponse(path, user)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/admin/crypto/networks") return json(route, { items: [network] })
    if (path.endsWith(`/admin/crypto/networks/${ids.network}/shadow`)) {
      if (route.request().method() === "POST") {
        const body: SetCryptoShadowControlRequest = route.request().postDataJSON()
        writes.push({ body, key: route.request().headers()["idempotency-key"] })
        if (failure === "conflict") {
          current = { ...current, paused: true, version: 2, leaseActive: false, status: "paused" }
          return json(route, { error: { code: "CONCURRENT_MODIFICATION", message: "Concurrent change" } }, 409)
        }
        if (failure === "retry" && writes.length === 1) return json(route, { error: { code: "INTERNAL_ERROR", message: "Temporary failure" } }, 503)
        current = { ...current, paused: body.paused, version: current.version + 1, status: body.paused ? "pausing" : "starting", leaseActive: body.paused }
        return json(route, current, 202)
      }
      return json(route, current)
    }
    return json(route, { items: [] })
  })
  await page.goto("/admin/crypto-networks")
  await expect(page.getByText("Теневая проверка блоков", { exact: true })).toBeVisible()
  return { writes, update: (state: Partial<CryptoShadowControlResponse>) => { current = { ...current, ...state } } }
}

test("financial activation exposes the current mode without offering a downgrade", async ({ page }) => {
  const state = await setup(page, { ...initial, mode: "hybrid", activatedAt: now, status: "financial", canChange: false, lockedReason: "financially_activated" })
  await expect(page.getByText("Финансовая обработка активирована", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Приостановить проверку", exact: true })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Возобновить проверку", exact: true })).toHaveCount(0)
  expect(state.writes).toHaveLength(0)
  await page.getByRole("group", { name: "Теневая проверка блоков", exact: true }).screenshot({ path: "../db/backup/shadow-control-ui-20261021/ui-financial.png" })
})

test("pause and resume use a reason and version and wait for the actual worker state", async ({ page }) => {
  const state = await setup(page)
  await page.getByRole("button", { name: "Приостановить проверку", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByRole("button", { name: "Подтвердить", exact: true })).toBeDisabled()
  await dialog.getByLabel("Причина действия", { exact: true }).fill("Плановая проверка")
  await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click()
  await expect(page.getByText("Приостанавливается", { exact: true })).toBeVisible()
  expect(state.writes[0].body).toEqual({ paused: true, version: 1, reason: "Плановая проверка" })
  expect(state.writes[0].key).toBeTruthy()
  await expect(page.getByRole("button", { name: "Возобновить проверку", exact: true })).toBeDisabled()
  state.update({ leaseActive: false, status: "paused" })
  await expect(page.getByRole("button", { name: "Возобновить проверку", exact: true })).toBeEnabled({ timeout: 12_000 })
  await page.getByRole("group", { name: "Теневая проверка блоков", exact: true }).screenshot({ path: "../db/backup/shadow-control-ui-20261021/ui-paused.png" })
  await page.getByRole("button", { name: "Возобновить проверку", exact: true }).click()
  await dialog.getByLabel("Причина действия", { exact: true }).fill("Проверка завершена")
  await dialog.getByRole("button", { name: "Подтвердить", exact: true }).click()
  await expect(page.getByText("Запускается", { exact: true })).toBeVisible()
  expect(state.writes[1].body).toEqual({ paused: false, version: 2, reason: "Проверка завершена" })
  state.update({ leaseActive: true, status: "running" })
  await expect(page.getByText("Включена", { exact: true })).toBeVisible({ timeout: 12_000 })
})

test("concurrent changes refresh the status without resubmitting the old command", async ({ page }) => {
  const state = await setup(page, initial, true, "conflict")
  await page.getByRole("button", { name: "Приостановить проверку", exact: true }).click()
  await page.getByLabel("Причина действия", { exact: true }).fill("Плановое обслуживание")
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click()
  await expect(page.getByRole("dialog")).toHaveCount(0)
  await expect(page.getByText("Состояние сети изменилось.", { exact: false })).toBeVisible()
  await expect(page.getByRole("button", { name: "Возобновить проверку", exact: true })).toBeEnabled()
  expect(state.writes).toHaveLength(1)
})

test("a retry without editing reuses the same idempotency key", async ({ page }) => {
  const state = await setup(page, initial, true, "retry")
  await page.getByRole("button", { name: "Приостановить проверку", exact: true }).click()
  await page.getByLabel("Причина действия", { exact: true }).fill("Повторяемая команда")
  await page.getByRole("button", { name: "Подтвердить", exact: true }).click()
  await expect(page.getByRole("button", { name: "Повторить запрос", exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Повторить запрос", exact: true }).click()
  await expect(page.getByRole("dialog")).toHaveCount(0)
  expect(state.writes).toHaveLength(2)
  expect(state.writes[0]).toEqual(state.writes[1])
})

test("an operator with update permission cannot use system administrator controls", async ({ page }) => {
  await setup(page, initial, false)
  await expect(page.getByText("Включена", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Приостановить проверку", exact: true })).toHaveCount(0)
})

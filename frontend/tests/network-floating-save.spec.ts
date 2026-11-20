import { expect, test, type Page } from "@playwright/test"
import type { CryptoNetworkStateResponse } from "../configuration/crypto/types/generated/http-contracts"
import type { CryptoNetworkPolicyPatch } from "../configuration/crypto/types/crypto-network"
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

// The floating copy lives beside the form, which needs an `xl` wide screen; short enough that the footer starts off screen.
test.use({ viewport: { width: 1440, height: 760 } })

async function setup(page: Page) {
  await installSession(page, admin)
  let current = structuredClone(network)
  const patches: CryptoNetworkPolicyPatch[] = []
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_network:read", "crypto_network:update"] })
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/admin/crypto/networks") return json(route, { items: [current] })
    if (path === `/api/v1/admin/crypto/networks/${ids.network}/policy` && route.request().method() === "PATCH") {
      const patch: CryptoNetworkPolicyPatch = route.request().postDataJSON()
      patches.push(patch)
      current = { ...current, policy: { ...current.policy, enabled: patch.enabled, version: current.policy.version + 1 } }
      return json(route, current)
    }
    return json(route, { items: [] })
  })
  await page.goto("/admin/crypto-networks")
  await expect(page.getByRole("switch", { name: "Обработка операций", exact: true })).toBeEnabled()
  return { patches }
}

const railState = (page: Page) => page.locator("[data-floating-save]")
const floatingSave = (page: Page) => page.getByRole("button", { name: "Сохранить настройки сети ETHEREUM-SEPOLIA", exact: true })
const footerSave = (page: Page) => page.getByRole("button", { name: "Сохранить настройки", exact: true })

test("an unsaved change brings a save action beside the form that follows the pointer lazily", async ({ page }) => {
  const state = await setup(page)
  await expect(footerSave(page)).not.toBeInViewport()
  await expect(railState(page)).toHaveAttribute("data-floating-save", "hidden")

  await page.getByRole("switch", { name: "Обработка операций", exact: true }).click()
  await expect(railState(page)).toHaveAttribute("data-floating-save", "shown")

  await page.mouse.move(400, 520)
  await expect.poll(async () => {
    const box = await floatingSave(page).boundingBox()
    return box ? Math.round(box.y + box.height / 2) : null
  }).toBe(520)

  // Working near the button does not move it.
  const center = async () => {
    const box = await floatingSave(page).boundingBox()
    return box ? Math.round(box.y + box.height / 2) : null
  }
  await page.mouse.move(400, 560)
  await page.waitForTimeout(700)
  expect(await center()).toBe(520)

  // Heading for the rail at another height leaves the target where it is, so it can be hit.
  const rail = (await page.locator("[data-floating-save]").boundingBox())!
  await page.mouse.move(rail.x + 20, 300, { steps: 4 })
  await page.waitForTimeout(700)
  expect(await center()).toBe(520)

  await floatingSave(page).click()
  await expect.poll(() => state.patches.length).toBe(1)
  expect(state.patches[0]).toMatchObject({ enabled: true })
  await expect(railState(page)).toHaveAttribute("data-floating-save", "hidden")
})

test("the copy steps aside once the form's own save button is on screen", async ({ page }) => {
  await setup(page)
  await page.getByRole("switch", { name: "Обработка операций", exact: true }).click()
  await expect(railState(page)).toHaveAttribute("data-floating-save", "shown")

  await footerSave(page).scrollIntoViewIfNeeded()
  await expect(footerSave(page)).toBeEnabled()
  await expect(railState(page)).toHaveAttribute("data-floating-save", "hidden")
  await expect(floatingSave(page)).toBeHidden()
})

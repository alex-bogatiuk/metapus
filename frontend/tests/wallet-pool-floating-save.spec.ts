import { expect, test, type Page } from "@playwright/test"
import type { WalletPoolPolicyPatch, WalletPoolState } from "../configuration/crypto/types/wallet-pool"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const admin = { ...erpUser, isAdmin: true }
const now = new Date().toISOString()
const pool: WalletPoolState = {
  policy: {
    networkId: ids.network, networkCode: "ETHEREUM-SEPOLIA", networkName: "Ethereum Sepolia", chainFamily: "evm", nativeTokenSymbol: "ETH", nativeDecimals: 18,
    enabled: true, lowWatermark: 5, targetReady: 10, maxTotalWallets: 100, maxProvisionBatch: 5,
    maxActiveInvoiceLeasesPerMerchant: 10, maxPersistentAddressesPerMerchant: 10, maxLeaseStartsPerMinutePerMerchant: 5, minGlobalReadyReserve: 2,
    activation: null, retryAfterSeconds: 30, addressReuseCooldownSeconds: 86400, maxWatcherLagSeconds: 300,
    derivationNamespace: "pool", needsReview: false, version: 1, updatedAt: now, updatedBy: "admin",
  },
  capabilities: { family: "evm", walletProvisioning: "derived" },
  stats: { ready: 10, cooldown: 0, leased: 0, assigned: 0, provisioning: 0, activating: 0, sweepPending: 0, held: 0, failed: 0, total: 10, inboxUnmatched: 0, inboxNeedsReview: 0, inboxPoison: 0 },
  state: "healthy",
}

// The floating copy needs an `xl` wide screen; short enough that the card's footer starts off screen.
test.use({ viewport: { width: 1440, height: 700 } })

const tronId = "20000000-0000-4000-8000-000000000001"
const tronPool: WalletPoolState = {
  ...structuredClone(pool),
  policy: { ...structuredClone(pool.policy), networkId: tronId, networkCode: "TRON-NILE", networkName: "TRON Nile", chainFamily: "tron", nativeTokenSymbol: "TRX", nativeDecimals: 6 },
  capabilities: { family: "tron", walletProvisioning: "derived" },
}

async function setup(page: Page, extra: WalletPoolState[] = []) {
  await installSession(page, admin)
  let current = structuredClone(pool)
  const patches: WalletPoolPolicyPatch[] = []
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["system:admin"] })
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/crypto/wallet-pools") return json(route, { items: [current, ...extra] })
    if (path.endsWith("/policy") && route.request().method() === "PATCH") {
      const patch: WalletPoolPolicyPatch = route.request().postDataJSON()
      patches.push(patch)
      current = { ...current, policy: { ...current.policy, targetReady: patch.targetReady, version: current.policy.version + 1 } }
      return json(route, current)
    }
    return json(route, { items: [] })
  })
  await page.goto("/settings")
  await page.getByRole("button", { name: /Пул кошельков/ }).first().click()
  await expect(page.getByText("Ethereum Sepolia", { exact: true })).toBeVisible()
  return { patches }
}

const railState = (page: Page) => page.locator("[data-floating-save]")
const floatingSave = (page: Page) => page.getByRole("button", { name: "Сохранить настройки пула Ethereum Sepolia", exact: true })
const footerSave = (page: Page) => page.getByRole("button", { name: "Сохранить настройки", exact: true })

test("an unsaved pool setting brings the floating save beside the card, and it saves", async ({ page }) => {
  const state = await setup(page)
  await expect(railState(page)).toHaveAttribute("data-floating-save", "hidden")

  const target = page.getByLabel("Целевое количество", { exact: true })
  await target.scrollIntoViewIfNeeded()
  await target.fill("12")
  await expect(footerSave(page)).not.toBeInViewport()
  await expect(railState(page)).toHaveAttribute("data-floating-save", "shown")

  await floatingSave(page).click()
  await expect.poll(() => state.patches.length).toBe(1)
  expect(state.patches[0]).toMatchObject({ targetReady: 12 })
  await expect(railState(page)).toHaveAttribute("data-floating-save", "hidden")
})

test("the floating save steps aside once the card's own save button is on screen", async ({ page }) => {
  await setup(page)
  const target = page.getByLabel("Целевое количество", { exact: true })
  await target.scrollIntoViewIfNeeded()
  await target.fill("12")
  await expect(railState(page)).toHaveAttribute("data-floating-save", "shown")

  await footerSave(page).scrollIntoViewIfNeeded()
  await expect(railState(page)).toHaveAttribute("data-floating-save", "hidden")
})

const toggle = (page: Page, verb: "Развернуть" | "Свернуть", name: string) => page.getByRole("button", { name: `${verb} пул ${name}`, exact: true })

test("several pools open collapsed, so TRON can be reached without scrolling past Sepolia", async ({ page }) => {
  await setup(page, [tronPool])
  await expect(toggle(page, "Развернуть", "Ethereum Sepolia")).toBeVisible()
  await expect(toggle(page, "Развернуть", "TRON Nile")).toBeVisible()
  await expect(page.getByLabel("Целевое количество", { exact: true })).toHaveCount(0)
  await expect(page.getByText("ETHEREUM-SEPOLIA · готово 10 из 10 · создание адресов включено", { exact: true })).toBeVisible()

  await toggle(page, "Развернуть", "TRON Nile").click()
  await expect(page.getByLabel("Целевое количество", { exact: true })).toHaveCount(1)
  await expect(toggle(page, "Развернуть", "Ethereum Sepolia")).toBeVisible()

  // The choice is remembered for the next visit.
  await page.reload()
  await page.getByRole("button", { name: /Пул кошельков/ }).first().click()
  await expect(toggle(page, "Свернуть", "TRON Nile")).toBeVisible()
  await expect(toggle(page, "Развернуть", "Ethereum Sepolia")).toBeVisible()

  await page.getByRole("button", { name: "Развернуть все", exact: true }).click()
  await expect(page.getByLabel("Целевое количество", { exact: true })).toHaveCount(2)
  await page.getByRole("button", { name: "Свернуть все", exact: true }).click()
  await expect(page.getByLabel("Целевое количество", { exact: true })).toHaveCount(0)
})

test("a pool opened after the page loads still hides its floating save at its own footer", async ({ page }) => {
  await setup(page, [tronPool])
  await toggle(page, "Развернуть", "Ethereum Sepolia").click()
  const target = page.getByLabel("Целевое количество", { exact: true })
  await target.scrollIntoViewIfNeeded()
  await target.fill("12")
  await expect(railState(page).first()).toHaveAttribute("data-floating-save", "shown")
  await footerSave(page).scrollIntoViewIfNeeded()
  await expect(railState(page).first()).toHaveAttribute("data-floating-save", "hidden")

  // Collapsed with unsaved changes, the card says so in its header.
  await toggle(page, "Свернуть", "Ethereum Sepolia").click()
  await expect(page.getByText("Не сохранено", { exact: true })).toBeVisible()
})

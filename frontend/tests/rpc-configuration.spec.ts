import { expect, test, type Page } from "@playwright/test"
import type { CryptoNetworkStateResponse, CryptoRPCConfigurationResponse, UpdateCryptoRPCConfigurationRequest } from "../types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const revision = "a".repeat(64)
const now = new Date().toISOString()
const endpointIds = [ids.user, ids.address, ids.wallet]
const defaultSettings: CryptoRPCConfigurationResponse["settings"] = {
  evm: { maxHeadAgeSeconds: 30, attemptTimeoutMs: 2000, readBudgetMs: 5000, logsTimeoutMs: 5000, maxAttempts: 2, providerConcurrency: 8, backfillConcurrency: 4, requestsPerSecond: 20, nativeConcurrency: 4, prefetchBlocks: 32, maxResponseBytes: 8388608, maxBufferedBytes: 268435456 },
  alerts: { maxLagSeconds: 60, fireAfterSeconds: 60, recoveryLagSeconds: 30, recoverAfterSeconds: 120 },
}
const initial: CryptoRPCConfigurationResponse = {
  family: "evm", settings: structuredClone(defaultSettings), effectiveSettings: structuredClone(defaultSettings), defaultSettings,
  networkId: ids.network, source: "database", revision, effectiveRevision: revision, canManage: true,
  endpoints: ["alchemy", "publicnode", "tenderly-secondary"].map((providerId, index) => ({ id: endpointIds[index], version: 1, name: providerId, role: "rpc", providerId, trustGroup: providerId === "tenderly-secondary" ? "tenderly" : providerId, isActive: true, priority: (index + 1) * 10, minRequestIntervalMs: 0 })),
  requirements: [{ role: "rpc", requiredProviders: 2, configuredProviders: 3 }],
}
const network: CryptoNetworkStateResponse = {
  network: { id: ids.network, code: "ETHEREUM-SEPOLIA", family: "evm", networkIdentifier: "11155111", nativeTokenId: ids.token, nativeTokenSymbol: "ETH", nativeDecimals: 18, requiredConfirmations: 12, finalityMode: "confirmations" },
  watching: true,
  capabilities: { family: "evm", walletProvisioning: "derived", requiredRpcRoles: ["rpc"], externalResourceMode: "none", sequenceModel: "nonce", checkpointTimeUnit: "block_height", tokenAccountModel: "same_address", minWatcherLagSeconds: 0, supportsReplacement: true },
  policy: { networkId: ids.network, enabled: false, perTransactionMaxFeeMinor: "30000000", dailyNetworkFeeBudgetMinor: "300000000", feeFunding: { mode: "source_only", dailyBudgetMinor: "0", minFeeWalletBalanceMinor: "0", minSourceWalletResidualMinor: "0", maxFundingAmountMinor: "0" }, retryDelaySeconds: 30, rpcFailureThreshold: 3, rpcOpenSeconds: 30, version: 1, updatedBy: "admin", createdAt: now, updatedAt: now },
  runtime: { networkId: ids.network, rpcEndpoints: initial.endpoints.map((endpoint, index) => ({ endpointId: endpoint.id, appliedRevision: revision, providerId: endpoint.providerId, role: "rpc", state: index === 2 ? "open" : "closed", failures: index === 2 ? 3 : 0, operations: index === 2 ? [{ operation: "full_block", state: "open", failures: 3, reasonCode: "timeout", retryAt: now }] : [] })), signerReady: true, resourceProviderReady: false, resourceProviderState: "unconfigured", observedAt: now, workerId: "fixture-worker", operationalHold: false },
}

async function setup(page: Page, scenario?: "conflict" | "lost" | "file" | "readonly" | "tron" | "automatic") {
  const user = { ...erpUser, isAdmin: scenario !== "readonly" }
  await installSession(page, user)
  let current = structuredClone(initial)
  const currentNetwork = structuredClone(network)
  if (scenario === "automatic") current.settings = { alerts: { maxLagSeconds: 0, fireAfterSeconds: 0, recoveryLagSeconds: 0, recoverAfterSeconds: 0 } }
  if (scenario === "tron") {
    current.family = "tron"
    currentNetwork.network.family = "tron"
    currentNetwork.network.code = "TRON-NILE"
    const tron = { ingestionMode: "hybrid", attemptTimeoutMs: 5000, readBudgetMs: 12000, maxAttempts: 2, providerConcurrency: 8, backfillConcurrency: 4, requestsPerSecond: 10, maxResponseBytes: 10485760 }
    current.defaultSettings = { tron, alerts: defaultSettings.alerts }
    current.settings = { tron: { ...tron, ingestionMode: "solidified", maxAttempts: 0, providerConcurrency: 0 }, alerts: defaultSettings.alerts }
    current.effectiveSettings = { tron: { ...tron, ingestionMode: "solidified" }, alerts: defaultSettings.alerts }
  }
  if (scenario === "file") current = { ...current, source: "file", canManage: false, lockedReason: "rpc_configuration_file", effectiveRevision: "" }
  if (scenario === "readonly") current = { ...current, canManage: false, lockedReason: "permission_denied" }
  const writes: { body: UpdateCryptoRPCConfigurationRequest; key: string }[] = []
  await page.route("**/api/v1/**", async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path === "/api/v1/auth/me/permissions") return json(route, { items: ["crypto_network:read", "catalog:crypto_rpc_endpoint:read"] })
    const common = commonAPIResponse(path, user)
    if (common !== undefined) return json(route, common)
    if (path === "/api/v1/admin/crypto/networks") return json(route, { items: [currentNetwork] })
    if (path.endsWith(`/networks/${ids.network}/rpc-configuration`)) {
      if (request.method() === "PATCH") {
        const body: UpdateCryptoRPCConfigurationRequest = request.postDataJSON()
        writes.push({ body, key: request.headers()["idempotency-key"] })
        if (scenario === "conflict") {
          current.revision = "b".repeat(64)
          return json(route, { code: "CONCURRENT_MODIFICATION", message: "Конфигурация изменена" }, 409)
        }
        if (scenario === "lost" && writes.length > 1) return json(route, current)
        current = { ...current, settings: body.settings ? { ...current.settings, ...body.settings } as CryptoRPCConfigurationResponse["settings"] : current.settings, revision: "b".repeat(64), effectiveRevision: "b".repeat(64), endpoints: current.endpoints.map(endpoint => {
          const change = body.endpoints?.find(item => item.id === endpoint.id)
          return change ? { ...endpoint, ...change, minRequestIntervalMs: change.minRequestIntervalMs ?? endpoint.minRequestIntervalMs, version: endpoint.version + 1 } : endpoint
        }).sort((a, b) => a.priority - b.priority) }
        if (scenario === "lost") return route.abort("connectionreset")
      }
      return json(route, current)
    }
    return json(route, { items: [] })
  })
  await page.goto("/admin/crypto-networks")
  if (scenario === "file") await expect(page.getByText("RPC этой сети настроены через файл.", { exact: false })).toBeVisible()
  else await expect(page.getByRole("group", { name: `RPC-подключения ${currentNetwork.network.code}`, exact: true })).toBeVisible()
  return writes
}

test("RPC reserve can be disabled and restored without removing its configuration", async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole("group", { name: "RPC-подключения ETHEREUM-SEPOLIA", exact: true }).screenshot({ path: "/private/tmp/metapus-rpc-management-desktop.png" })
  await page.getByRole("button", { name: "Диагностика tenderly-secondary", exact: true }).click()
  await expect(page.getByRole("cell", { name: "Провайдер не ответил вовремя", exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByRole("switch", { name: "Использовать tenderly-secondary", exact: true }).click()
  expect(writes).toHaveLength(0)
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect(page.getByRole("link", { name: "tenderly-secondary", exact: true })).toHaveCount(0)
  expect(writes).toHaveLength(1)
  expect(writes[0].body.endpoints).toEqual([{ id: ids.wallet, version: 1, isActive: false, priority: 30 }])
  await page.getByRole("switch", { name: "Показывать отключённые (1)", exact: true }).click()
  await expect(page.getByText("Отключён администратором", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByRole("switch", { name: "Использовать tenderly-secondary", exact: true }).click()
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect.poll(() => writes.length).toBe(2)
  expect(writes[1].body.endpoints).toEqual([{ id: ids.wallet, version: 2, isActive: true, priority: 30 }])
  await expect(page.getByRole("link", { name: "tenderly-secondary", exact: true })).toBeVisible()
})

test("RPC reordering is staged and survives collapsing the network card", async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByRole("button", { name: "Выше: tenderly-secondary", exact: true }).click()
  await page.getByRole("button", { name: "Свернуть сеть ETHEREUM-SEPOLIA", exact: true }).click()
  await page.getByRole("button", { name: "Развернуть сеть ETHEREUM-SEPOLIA", exact: true }).click()
  await expect(page.getByRole("button", { name: "Применить RPC", exact: true })).toBeEnabled()
  expect(writes).toHaveLength(0)
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].body.endpoints?.map(row => [row.id, row.priority])).toEqual([[ids.wallet, 20], [ids.address, 30]])
})

test("RPC editor prevents removing required independent sources", async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  for (const provider of ["alchemy", "publicnode"]) await page.getByRole("switch", { name: `Использовать ${provider}`, exact: true }).click()
  await expect(page.getByRole("button", { name: "Применить RPC", exact: true })).toBeDisabled()
  await expect(page.getByText("Недостаточно независимых источников", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Отменить", exact: true }).click()
  expect(writes).toHaveLength(0)
})

test("RPC version conflict requires an explicit refresh instead of overwriting changes", async ({ page }) => {
  const writes = await setup(page, "conflict")
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByRole("switch", { name: "Использовать tenderly-secondary", exact: true }).click()
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect(page.getByRole("button", { name: "Загрузить актуальные настройки", exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Применить RPC", exact: true })).toBeDisabled()
  expect(writes).toHaveLength(1)
})

test("lost RPC mutation response freezes the draft and reuses its idempotency key", async ({ page }) => {
  const writes = await setup(page, "lost")
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByRole("switch", { name: "Использовать tenderly-secondary", exact: true }).click()
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect(page.getByRole("switch", { name: "Использовать tenderly-secondary", exact: true })).toBeDisabled()
  await page.getByRole("button", { name: "Повторить запрос", exact: true }).click()
  await expect(page.getByRole("button", { name: "Настроить RPC", exact: true })).toBeVisible()
  expect(writes).toHaveLength(2)
  expect(writes[0]).toEqual(writes[1])
})

for (const scenario of ["file", "readonly"] as const) test(`RPC configuration is read-only in ${scenario} mode`, async ({ page }) => {
  await setup(page, scenario)
  await expect(page.getByRole("button", { name: "Настроить RPC", exact: true })).toHaveCount(0)
})

test("RPC controls remain usable in a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const writes = await setup(page)
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByRole("switch", { name: "Использовать tenderly-secondary", exact: true }).click()
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  const section = page.getByRole("group", { name: "RPC-подключения ETHEREUM-SEPOLIA", exact: true })
  const bounds = await section.boundingBox()
  expect(bounds?.width).toBeLessThanOrEqual(390)
  expect(bounds?.x).toBeGreaterThanOrEqual(0)
  await section.screenshot({ path: "/private/tmp/metapus-rpc-management-mobile.png" })
})


test("RPC timeouts, notification thresholds and pacing are saved together", async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByLabel("Таймаут одной попытки, мс", { exact: true }).fill("10000")
  await expect(page.getByText("Бюджет должен вмещать как минимум две попытки RPC", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Применить RPC", exact: true })).toBeDisabled()
  await page.getByLabel("Бюджет операции с резервом, мс", { exact: true }).fill("25000")
  await page.getByLabel("Допустимое отставание, с", { exact: true }).fill("120")
  await page.getByLabel("Длительность проблемы, с", { exact: true }).fill("90")
  await page.getByLabel("Отставание для восстановления, с", { exact: true }).fill("120")
  await expect(page.getByRole("button", { name: "Применить RPC", exact: true })).toBeDisabled()
  await page.getByLabel("Отставание для восстановления, с", { exact: true }).fill("20")
  await page.getByLabel("Длительность восстановления, с", { exact: true }).fill("180")
  const row = page.getByRole("row").filter({ has: page.getByRole("link", { name: "alchemy", exact: true }) })
  await row.getByLabel("Интервал, мс", { exact: true }).fill("500")
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].body.settings?.evm?.attemptTimeoutMs).toBe(10000)
  expect(writes[0].body.settings?.evm?.readBudgetMs).toBe(25000)
  expect(writes[0].body.settings?.alerts).toEqual({ maxLagSeconds: 120, fireAfterSeconds: 90, recoveryLagSeconds: 20, recoverAfterSeconds: 180 })
  expect(writes[0].body.endpoints?.[0].minRequestIntervalMs).toBe(500)
  await expect(page.getByRole("button", { name: "Настроить RPC", exact: true })).toBeVisible()
})

test("RPC automatic values remain explicit and advanced memory limits are validated", async ({ page }) => {
  const writes = await setup(page)
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByLabel("Таймаут одной попытки, мс", { exact: true }).fill("0")
  await expect(page.getByText("Автоматически: 2000 мс", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "Дополнительные настройки RPC", exact: true }).click()
  await page.getByLabel("Память предзагрузки EVM, байт", { exact: true }).fill("8388608")
  await expect(page.getByText("Памяти должно хватать на все одновременные ответы", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Применить RPC", exact: true })).toBeDisabled()
  await page.getByLabel("Память предзагрузки EVM, байт", { exact: true }).fill("0")
  await page.getByLabel("Таймаут одной попытки, мс", { exact: true }).scrollIntoViewIfNeeded()
  await page.screenshot({ path: "/private/tmp/metapus-rpc-settings-20261029/desktop.png" })
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].body.settings?.evm?.attemptTimeoutMs).toBe(0)
  expect(writes[0].body.settings?.evm?.maxBufferedBytes).toBe(0)
})


test("TRON settings display automatic defaults and preserve the controlled ingestion mode", async ({ page }) => {
  const writes = await setup(page, "tron")
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByLabel("Таймаут одной попытки, мс", { exact: true }).fill("8000")
  await page.getByLabel("Бюджет операции с резервом, мс", { exact: true }).fill("18000")
  await page.getByRole("button", { name: "Дополнительные настройки RPC", exact: true }).click()
  await expect(page.getByText("Автоматически: 8", { exact: true })).toBeVisible()
  await expect(page.getByLabel("Таймаут поиска событий EVM, мс", { exact: true })).toHaveCount(0)
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].body.settings?.tron?.ingestionMode).toBe("solidified")
  expect(writes[0].body.settings?.tron?.providerConcurrency).toBe(0)
  expect(writes[0].body.settings?.evm).toBeUndefined()
})


test("editing notification thresholds alone leaves automatic RPC settings absent", async ({ page }) => {
  const writes = await setup(page, "automatic")
  await page.getByRole("button", { name: "Настроить RPC", exact: true }).click()
  await page.getByLabel("Допустимое отставание, с", { exact: true }).fill("120")
  await page.getByRole("button", { name: "Применить RPC", exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0].body.settings?.evm).toBeUndefined()
  expect(writes[0].body.settings?.tron).toBeUndefined()
  expect(writes[0].body.settings?.alerts?.maxLagSeconds).toBe(120)
  expect(writes[0].body.endpoints).toEqual([])
})

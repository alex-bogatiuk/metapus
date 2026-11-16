import type { JsonObject } from "@/lib/json-value"
import { expect, test, type Page } from "@playwright/test"
import type { CryptoRPCEndpointResponse, IntegrationRPCKeyOptionsResponse } from "@/configuration/crypto/types/generated/http-contracts"
import { commonAPIResponse, erpUser, ids, installSession, json } from "./helpers"

const at = "2026-11-12T10:00:00Z"
const network = "11155111-0000-4000-8000-000000000001"
const meta = {
  name: "CryptoRPCEndpoint", key: "crypto_rpc_endpoint", type: "catalog", routePrefix: "crypto-rpc-endpoints",
  presentation: { singular: "Подключение RPC", plural: "Подключения RPC" },
  fields: [
    { name: "code", type: "string", label: "Код" },
    { name: "name", type: "string", label: "Наименование" },
    { name: "url", type: "string", label: "Адрес RPC" },
    { name: "poolId", type: "reference", label: "Пул аккаунтов", referenceType: "integration_pool" },
    { name: "credentialSlot", type: "string", label: "Имя ключа" },
  ],
}

async function setup(page: Page, endpoint: CryptoRPCEndpointResponse, options: IntegrationRPCKeyOptionsResponse) {
  const admin = { ...erpUser, isAdmin: true }
  await installSession(page, admin)
  const saved: JsonObject[] = []
  const queries: URLSearchParams[] = []
  const pool = { id: ids.wallet, code: "POOL", name: "Пул", provider: "infura", isActive: true, accountIds: [], version: 1, isFolder: false, deletionMark: false, attributes: {}, createdAt: at, updatedAt: at }
  await page.route("**/api/v1/**", route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path === "/api/v1/meta/entities") return json(route, [meta, { name: "IntegrationPool", key: "integration_pool", type: "catalog", routePrefix: "integration-pools", fields: [], presentation: { singular: "Пул", plural: "Пулы" } }])
    if (path.startsWith("/api/v1/meta/")) return json(route, meta)
    const common = commonAPIResponse(path, admin)
    if (common !== undefined) return json(route, common)
    if (path === `/api/v1/system/integrations/pools/${ids.wallet}/rpc-key-names`) { queries.push(url.searchParams); return json(route, options) }
    if (path === `/api/v1/catalog/crypto-rpc-endpoints/${endpoint.id}`) {
      if (request.method() === "PUT") { const body = request.postDataJSON() as JsonObject; saved.push(body); return json(route, { ...endpoint, ...body, version: endpoint.version + 1 }) }
      return json(route, endpoint)
    }
    if (path.endsWith(`/integration-pools/${ids.wallet}`)) return json(route, pool)
    if (path.endsWith("/integration-pools")) return json(route, { items: [pool], hasMore: false, hasPrev: false, totalCount: 1 })
    return json(route, { items: [] })
  })
  return { saved, queries }
}

const base: CryptoRPCEndpointResponse = {
  id: ids.merchant, code: "ETHEREUM-SEPOLIA/rpc/infura-sepolia", name: "ETHEREUM-SEPOLIA/rpc/infura-sepolia", isFolder: false, deletionMark: false, version: 3, attributes: {},
  networkId: network, role: "rpc", providerId: "infura-sepolia", trustGroup: "infura", url: "https://sepolia.infura.io/v3/", poolId: ids.wallet,
  credentialSlot: "default", priority: 5, minRequestIntervalMs: 0, disableKeepAlives: false, isActive: true,
}

test("a provider-fixed key name replaces a stale value and warns about accounts without it", async ({ page }) => {
  const { saved, queries } = await setup(page, base, {
    required: "sepolia", names: [],
    accounts: [{ id: ids.user, name: "Infura gamearena0", hasCredentials: true, keys: ["sepolia"] }, { id: ids.token, name: "Infura old", hasCredentials: true, keys: ["default"] }, { id: ids.wallet, name: "Infura empty", hasCredentials: false, keys: [] }],
  })
  await page.goto(`/catalogs/crypto-rpc-endpoints/${base.id}`)
  const field = page.getByLabel("Имя ключа", { exact: true })
  await expect(field).toHaveValue("sepolia")
  await expect(field).toHaveAttribute("readonly", "")
  await expect(page.getByText("Задаётся профилем провайдера для этой сети и роли.")).toBeVisible()
  await expect(page.getByText("В аккаунте «Infura old» нет ключа «sepolia».", { exact: false })).toBeVisible()
  expect(queries[0].get("networkId")).toBe(network)
  expect(queries[0].get("role")).toBe("rpc")
  expect(queries[0].get("url")).toBe(base.url)
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => saved.length).toBe(1)
  expect(saved[0].credentialSlot).toBe("sepolia")
})

test("a provider without a fixed name offers only keys every keyed pool account has", async ({ page }) => {
  const alchemy = { ...base, providerId: "alchemy-sepolia", trustGroup: "alchemy", url: "https://eth-sepolia.g.alchemy.com/v2/", credentialSlot: "nile" }
  const { saved } = await setup(page, alchemy, {
    required: "", names: ["nile", "sepolia"],
    accounts: [{ id: ids.user, name: "Alchemy A", hasCredentials: true, keys: ["nile", "sepolia"] }, { id: ids.token, name: "Alchemy B", hasCredentials: true, keys: ["nile", "sepolia", "spare"] }],
  })
  await page.goto(`/catalogs/crypto-rpc-endpoints/${alchemy.id}`)
  const field = page.getByRole("combobox", { name: "Имя ключа" })
  await expect(field).toHaveText("nile")
  await field.click()
  await expect(page.getByRole("option")).toHaveText(["nile", "sepolia"])
  await page.getByRole("option", { name: "sepolia", exact: true }).click()
  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => saved.length).toBe(1)
  expect(saved[0].credentialSlot).toBe("sepolia")
})

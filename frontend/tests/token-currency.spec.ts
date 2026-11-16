import type { JsonObject } from "@/lib/json-value"
import { expect, test } from "@playwright/test"
import { commonAPIResponse, installSession, json } from "./helpers"
import { buildCatalogFormPayload } from "../lib/catalog-form-payload"

const tokenId = "11155111-0000-4000-8000-000000000003"
const networkId = "11155111-0000-4000-8000-000000000001"
const currencyId = "01a0771b-c4ed-7bbf-be93-97b80b171c55"
const currency = { id: currencyId, code: "PYUSD", name: "PayPal USD", isoCode: null, symbol: null, decimalPlaces: 6,
  isBase: false, isFolder: false, deletionMark: false, version: 1 } satisfies import("../types/generated/http-contracts").CurrencyResponse
const path = `/api/v1/catalog/tokens/${tokenId}`
const editableToken = {
  code: "PYUSD-SEPOLIA", name: "PayPal USD", networkId,
  contractAddress: "0xCaC524BcA292aaade2DF8A05cC58F0a65B1B3bB9",
  symbol: "PYUSD", decimalPlaces: 6, tokenStandard: "ERC-20", isActive: true,
  sweepThreshold: "1000000", sweepMaxAgeHours: 1, sweepRetainAmount: "0",
  isFolder: false, attributes: { note: "Preserve custom attributes" },
}
const tokenMetadata = {
  key: "token", name: "Token", type: "catalog", routePrefix: "tokens",
  presentation: { singular: "Токен", plural: "Токены", new: "Новый токен" },
  fields: [
    { name: "id", type: "string", readOnly: true },
    { name: "version", type: "integer" },
    { name: "deletionMark", type: "boolean" },
    { name: "createdAt", type: "date", readOnly: true },
    { name: "attributes", type: "json" },
    ...Object.entries(editableToken).filter(([name]) => name !== "attributes").map(([name, value]) => ({
      name, type: typeof value === "number" ? "integer" : typeof value,
    })),
    { name: "currencyId", label: "Валюта", type: "reference", referenceType: "currency" },
  ],
}

test("new catalog payload excludes copied identity and version without changing the draft", () => {
  const draft = { ...editableToken, id: tokenId, version: 7, deletionMark: false, currencyId }
  expect(buildCatalogFormPayload(tokenMetadata.fields, draft, true)).toEqual({ ...editableToken, currencyId })
  expect(draft.id).toBe(tokenId)
  expect(draft.version).toBe(7)
})

test("catalog payload preserves cleared values, large amount strings and write-only field omission", () => {
  const fields = [...tokenMetadata.fields, { name: "apiKey" }, { name: "calculated", readOnly: true }]
  const draft = {
    ...editableToken, version: 7, currencyId: null, isActive: false,
    sweepThreshold: "123456789012345678901234567890", calculated: "server value", apiKeyConfigured: true,
  }
  const expected = { ...editableToken, version: 7, currencyId: null, isActive: false, sweepThreshold: draft.sweepThreshold }
  expect(buildCatalogFormPayload(fields, draft, false)).toEqual(expected)
  expect(buildCatalogFormPayload(fields, { ...draft, apiKey: "" }, false)).toEqual({ ...expected, apiKey: "" })
})

test("catalog payload excludes form-specific hidden requisites", () => {
  const fields = [
    { name: "name" },
    { name: "baseUrl" },
    { name: "priority" },
    { name: "apiKey" },
  ]
  const draft = {
    name: "CoinGecko",
    baseUrl: "https://api.coingecko.com/api/v3",
    priority: 100,
    apiKey: "rotated-demo-key",
    apiKeyConfigured: true,
    version: 4,
  }

  expect(buildCatalogFormPayload(fields, draft, false, ["baseUrl", "priority"])).toEqual({
    name: "CoinGecko",
    apiKey: "rotated-demo-key",
    version: 4,
  })
})

test("token currency can be selected and saved repeatedly with a strict update body", async ({ page }) => {
  await installSession(page)
  const requests: JsonObject[] = []
  let token: import("../configuration/crypto/types/generated/http-contracts").CryptoTokenResponse = {
    ...editableToken, id: tokenId, version: 7, deletionMark: false,
    network: { id: networkId, code: "ETHEREUM-SEPOLIA", name: "Ethereum Sepolia" },
  }
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === "/api/v1/meta/entities") return json(route, [
      tokenMetadata,
      { key: "currency", name: "Currency", type: "catalog", routePrefix: "currencies", presentation: { singular: "Валюта", plural: "Валюты" } },
    ])
    if (url.pathname.startsWith("/api/v1/meta/")) return json(route, tokenMetadata)
    const common = commonAPIResponse(url.pathname)
    if (common !== undefined) return json(route, common)
    if (url.pathname === path) {
      if (request.method() === "PUT") {
        const body = request.postDataJSON() as JsonObject
        requests.push(body)
        const allowed = new Set([...Object.keys(editableToken), "currencyId", "version"])
        if (Object.keys(body).some((key) => !allowed.has(key))) {
          return json(route, { code: "VALIDATION_ERROR", message: "invalid request body" }, 400)
        }
        token = { ...token, currencyId, currency: { id: currencyId, name: "PayPal USD" }, version: token.version + 1 }
      }
      return json(route, token)
    }
    if (url.pathname === "/api/v1/catalog/currencies") return json(route, { items: [currency], totalCount: 1, hasMore: false, hasPrev: false })
    if (url.pathname === `/api/v1/catalog/currencies/${currencyId}`) return json(route, currency)
    return json(route, { items: [], total: 0 })
  })
  await page.goto(`/catalogs/tokens/${tokenId}`)
  const currencyField = page.locator('label[for="currencyId"]').locator("..").getByRole("combobox")
  await currencyField.click()
  await page.getByRole("option", { name: /PayPal USD/ }).click()
  await expect(currencyField).toContainText("PayPal USD")

  for (const version of [7, 8]) {
    await page.getByRole("button", { name: "Записать", exact: true }).click()
    await expect.poll(() => requests.length).toBe(version - 6)
    expect(requests.at(-1)).toEqual({ ...editableToken, currencyId, version })
    await expect(page.getByText("Записано", { exact: true }).first()).toBeVisible()
  }
  expect(token.currencyId).toBe(currencyId)
})

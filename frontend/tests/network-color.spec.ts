import type { JsonObject } from "@/lib/json-value"
import { expect, test } from "@playwright/test"
import { commonAPIResponse, installSession, json } from "./helpers"

const networkId = "11155111-0000-4000-8000-000000000001"
const path = `/api/v1/catalog/blockchain-networks/${networkId}`
const palette = [
  ["red", "Красный"], ["orange", "Оранжевый"], ["amber", "Янтарный"], ["yellow", "Жёлтый"], ["lime", "Лаймовый"],
  ["green", "Зелёный"], ["teal", "Бирюзовый"], ["sky", "Голубой"], ["blue", "Синий"], ["indigo", "Индиго"],
  ["violet", "Фиолетовый"], ["pink", "Розовый"], ["gray", "Серый"],
].map(([value, label]) => ({ value, label }))
const metadata = {
  key: "blockchain_network", name: "BlockchainNetwork", type: "catalog", routePrefix: "blockchain-networks",
  presentation: { singular: "Блокчейн-сеть", plural: "Блокчейн-сети" },
  fields: [
    { name: "id", type: "string", readOnly: true },
    { name: "version", type: "integer" },
    { name: "code", label: "Код", type: "string" },
    { name: "name", label: "Наименование", type: "string", required: true },
    { name: "rpcConfigSource", type: "string" },
    { name: "chainFamily", type: "enum", enumValues: [{ value: "evm", label: "EVM" }] },
    { name: "networkIdentifier", type: "string" },
    { name: "nativeTokenSymbol", type: "string" },
    { name: "nativeDecimals", type: "integer" },
    { name: "confirmationsNeeded", type: "integer" },
    { name: "blockTimeSeconds", type: "integer" },
    { name: "explorerUrl", type: "string" },
    { name: "color", label: "Цвет", type: "enum", widget: "color", enumValues: palette },
    { name: "isActive", type: "boolean", readOnly: true },
    { name: "isFolder", type: "boolean" },
  ],
}

test("a network colour is picked from swatches with the pointer or the keyboard and saved as a palette key", async ({ page }) => {
  await installSession(page)
  const saved: JsonObject[] = []
  let network: import("../types/generated/http-contracts").BlockchainNetworkResponse = {
    id: networkId, code: "ETHEREUM-SEPOLIA", name: "Ethereum Sepolia", rpcConfigSource: "file", chainFamily: "evm",
    networkIdentifier: "11155111", nativeTokenSymbol: "ETH", nativeDecimals: 18, confirmationsNeeded: 12, blockTimeSeconds: 12,
    explorerUrl: "", color: "", isActive: false, isFolder: false, deletionMark: false, version: 3,
  }
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === "/api/v1/meta/entities") return json(route, [metadata])
    if (url.pathname.startsWith("/api/v1/meta/")) return json(route, metadata)
    const common = commonAPIResponse(url.pathname)
    if (common !== undefined) return json(route, common)
    if (url.pathname === path) {
      if (request.method() === "PUT") {
        const body = request.postDataJSON() as JsonObject
        saved.push(body)
        network = { ...network, color: String(body.color), version: network.version + 1 }
      }
      return json(route, network)
    }
    return json(route, { items: [], total: 0 })
  })

  await page.goto(`/catalogs/blockchain-networks/${networkId}`)
  const trigger = page.locator("#color")
  await expect(trigger).toHaveText("Автоматически")

  await trigger.click()
  const swatches = page.getByRole("radiogroup", { name: "Цвет" })
  await expect(swatches.getByRole("radio")).toHaveCount(palette.length + 1)
  await expect(swatches.getByRole("radio", { name: "Автоматически" })).toBeFocused()
  await swatches.getByRole("radio", { name: "Синий" }).click()
  await expect(swatches).toBeHidden()
  await expect(trigger).toHaveText("Синий")

  // Keyboard: opening focuses the selected swatch, arrows walk the grid, Escape keeps the value.
  await trigger.focus()
  await page.keyboard.press("Enter")
  await expect(swatches.getByRole("radio", { name: "Синий" })).toBeFocused()
  await expect(swatches.getByRole("radio", { name: "Синий" })).toHaveAttribute("aria-checked", "true")
  await page.keyboard.press("ArrowLeft")
  await page.keyboard.press("ArrowLeft")
  await expect(swatches.getByRole("radio", { name: "Бирюзовый" })).toBeFocused()
  await page.keyboard.press("Escape")
  await expect(trigger).toHaveText("Синий")
  await trigger.click()
  await expect(swatches.getByRole("radio", { name: "Синий" })).toBeFocused()
  await page.keyboard.press("ArrowLeft")
  await page.keyboard.press("ArrowLeft")
  await page.keyboard.press("Enter")
  await expect(trigger).toHaveText("Бирюзовый")

  await page.getByRole("button", { name: "Записать", exact: true }).click()
  await expect.poll(() => saved.at(-1)?.color).toBe("teal")
})

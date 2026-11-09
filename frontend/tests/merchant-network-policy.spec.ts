import { expect, test } from "@playwright/test"

import { commonAPIResponse, ids, installPortalSession, json, portalOwner, portalRoles } from "./helpers"

const allowedNetwork = {
  id: "10000000-0000-4000-8000-000000000011",
  network: "203.0.113.24/32",
  label: "recovery",
  isActive: true,
  createdAt: "2026-08-30T09:00:00Z",
}

test("merchant owner stages CIDR policy and every mutation is idempotent", async ({ page }) => {
  await installPortalSession(page)
  let mode: "off" | "warn" | "enforce" = "off"
  let networks: (typeof allowedNetwork)[] = []
  const mutationKeys: string[] = []

  await page.route("**/api/v1/**", async (route) => {
    const pathname = new URL(route.request().url()).pathname
    const common = commonAPIResponse(pathname, portalOwner)
    return json(route, common ?? { items: [] })
  })
  await page.route("**/portal/v1/**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (request.method() !== "GET") mutationKeys.push(request.headers()["idempotency-key"] ?? "")

    if (url.pathname === "/portal/v1/merchants") {
      return json(route, { items: [{ id: ids.merchant, name: "Acme", code: "ACME", role: portalRoles.owner, networkAccess: "allow" }] })
    }
    if (url.pathname === "/portal/v1/settings") return json(route, { defaultTtlMinutes: 60 })
    if (url.pathname === "/portal/v1/settings/fees") return json(route, { items: [] })
    if (url.pathname === "/portal/v1/api-keys") return json(route, { items: [], total: 0 })
    if (url.pathname === "/portal/v1/security/network-policies/portal/allowed-networks" && request.method() === "POST") {
      networks = [allowedNetwork]
      return json(route, policy())
    }
    if (url.pathname === "/portal/v1/security/network-policies/portal") {
      if (request.method() === "PUT") {
        const body = request.postDataJSON() as { mode: typeof mode }
        mode = body.mode
        return json(route, policy())
      }
      return json(route, { policy: policy(), detectedIp: "203.0.113.24", networkAccess: "allow" })
    }
    return json(route, { items: [] })
  })

  function policy() {
    return {
      id: "10000000-0000-4000-8000-000000000010",
      merchantId: ids.merchant,
      surface: "portal",
      mode,
      version: mode === "off" ? 1 : mode === "warn" ? 2 : 3,
      allowedNetworks: networks,
      updatedAt: "2026-08-30T09:00:00Z",
    }
  }

  await page.goto("/portal/settings")
  await expect(page.getByRole("heading", { name: "Сетевой доступ" })).toBeVisible()
  await expect(page.getByText("Определённый IP: 203.0.113.24")).toBeVisible()

  await page.getByLabel("Причина изменения").fill("keep owner recovery path")
  await page.getByLabel("CIDR").fill("203.0.113.24/32")
  await page.getByLabel("Метка").fill("recovery")
  await page.getByRole("button", { name: "Добавить сеть" }).click()
  await expect(page.getByText("203.0.113.24/32")).toBeVisible()

  await page.getByLabel("Причина изменения").fill("observe before enforcement")
  await page.getByLabel("Режим").click()
  await page.getByRole("option", { name: "Только предупреждать" }).click()
  await expect(page.getByLabel("Режим")).toContainText("Только предупреждать")

  await page.getByLabel("Причина изменения").fill("owner recovery network verified")
  await page.getByLabel("Режим").click()
  await page.getByRole("option", { name: "Запрещать доступ" }).click()
  await expect(page.getByLabel("Режим")).toContainText("Запрещать доступ")

  expect(mutationKeys).toHaveLength(3)
  expect(new Set(mutationKeys).size).toBe(3)
  for (const key of mutationKeys) expect(key).toMatch(/^[0-9a-f-]{36}$/i)
})

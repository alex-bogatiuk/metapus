import type { JsonObject } from "@/lib/json-value"
import { expect, test } from "@playwright/test"

import type { PortalWebhookEndpoint } from "../configuration/crypto/types/portal-api"
import { commonAPIResponse, ids, installPortalSession, json, portalOwner, portalRoles } from "./helpers"

const endpointId = "10000000-0000-4000-8000-000000000020"
const eventId = "10000000-0000-4000-8000-000000000021"

function merchant(role: number) {
  return {
    items: [{ id: ids.merchant, name: "Acme", code: "ACME", role, networkAccess: "allow" as const }],
  }
}

const baseEndpoint = {
  id: endpointId,
  object: "webhookEndpoint" as const,
  name: "Production",
  url: "https://merchant.example.test/webhooks/metapus",
  enabledEvents: ["invoice.completed"],
  isDefault: true,
  isActive: true,
  secretVersion: 1,
  version: 1,
  createdAt: "2026-08-30T10:00:00Z",
  updatedAt: "2026-08-30T10:00:00Z",
}

const terminalEvent = {
  id: eventId,
  object: "event" as const,
  type: "invoice.completed",
  apiVersion: "2026-08-30",
  resourceVersion: 4,
  invoiceId: ids.adjustment,
  createdAt: "2026-08-30T11:00:00Z",
  payload: { id: eventId, object: "event", apiVersion: "2026-08-30", type: "invoice.completed", createdAt: "2026-08-30T11:00:00Z",
    data: { object: { id: ids.adjustment, status: "completed", amount: "12500000" } } },
} satisfies import("../types/generated/http-contracts").MerchantEventResponse

async function installCommonRoutes(page: Parameters<typeof installPortalSession>[0]) {
  await page.route("**/api/v1/**", async (route) => {
    const pathname = new URL(route.request().url()).pathname
    return json(route, commonAPIResponse(pathname, portalOwner) ?? { items: [] })
  })
}

test("owner rotates secret and redelivers one terminal event with durable keys", async ({ page }) => {
  await installPortalSession(page)
  await installCommonRoutes(page)
  const mutationKeys: string[] = []
  let endpoint: PortalWebhookEndpoint = { ...baseEndpoint }
  let redeliveryBody: JsonObject | undefined

  await page.route("**/portal/v1/**", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === "/portal/v1/merchants") return json(route, merchant(portalRoles.owner))
    if (pathname === "/portal/v1/webhook-endpoints" && request.method() === "GET") {
      return json(route, { object: "list", items: [endpoint], total: 1 })
    }
    if (pathname === `/portal/v1/webhook-endpoints/${endpointId}/rotate-secret`) {
      mutationKeys.push(request.headers()["idempotency-key"] ?? "")
      endpoint = {
        ...endpoint,
        secretVersion: 2,
        version: 2,
        updatedAt: "2026-08-30T11:05:00Z",
        previousSecretValidUntil: "2026-08-31T11:05:00Z",
      }
      return json(route, { ...endpoint, secret: "whsec_test_rotated_once" })
    }
    if (pathname === "/portal/v1/events") return json(route, { object: "list", items: [terminalEvent], total: 1 })
    if (pathname === `/portal/v1/events/${eventId}/deliveries`) {
      return json(route, { object: "list", items: [{
        id: "10000000-0000-4000-8000-000000000022",
        invoiceId: ids.adjustment, merchantId: ids.merchant, eventId,
        eventType: terminalEvent.type,
        deliveryId: "delivery-terminal-failed",
        statusCode: 503,
        responseTimeMs: 800,
        attempt: 10,
        errorMessage: "retry window exhausted",
        errorClass: "http_5xx",
        responseBody: "unavailable",
        webhookUrl: endpoint.url,
        createdAt: "2026-08-30T11:10:00Z",
      }], total: 1 })
    }
    if (pathname === `/portal/v1/events/${eventId}/redeliver`) {
      mutationKeys.push(request.headers()["idempotency-key"] ?? "")
      redeliveryBody = request.postDataJSON() as JsonObject
      return json(route, { status: "queued" })
    }
    return json(route, { object: "list", items: [] })
  })

  await page.goto("/portal/developers/webhooks")
  await expect(page.getByText("Secret v1")).toBeVisible()
  await page.getByRole("button", { name: "Secret" }).click()
  await expect(page.getByText("whsec_test_rotated_once", { exact: true })).toBeVisible()
  await expect(page.getByText(/Secret v2/)).toBeVisible()

  await page.getByRole("button", { name: "Открыть" }).click()
  await expect(page.getByText(/Попытка 10 · HTTP 503/)).toBeVisible()
  await page.getByRole("button", { name: "Переотправить" }).click()
  await expect.poll(() => mutationKeys.length).toBe(2)

  expect(new Set(mutationKeys).size).toBe(2)
  for (const key of mutationKeys) expect(key).toMatch(/^[0-9a-f-]{36}$/i)
  expect(redeliveryBody).toEqual({ endpointId })
})

test("manager has read-only registry access and cannot trigger owner mutations", async ({ page }) => {
  await installPortalSession(page)
  await installCommonRoutes(page)
  let mutationCount = 0

  await page.route("**/portal/v1/**", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (request.method() !== "GET") mutationCount += 1
    if (pathname === "/portal/v1/merchants") return json(route, merchant(portalRoles.manager))
    if (pathname === "/portal/v1/webhook-endpoints") return json(route, { object: "list", items: [baseEndpoint], total: 1 })
    if (pathname === "/portal/v1/events") return json(route, { object: "list", items: [terminalEvent], total: 1 })
    if (pathname === `/portal/v1/events/${eventId}/deliveries`) return json(route, { object: "list", items: [], total: 0 })
    return json(route, { object: "list", items: [] })
  })

  await page.goto("/portal/developers/webhooks")
  await expect(page.getByText("Режим чтения")).toBeVisible()
  await expect(page.getByText("Production", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Secret" })).toHaveCount(0)
  await page.getByRole("button", { name: "Открыть" }).click()
  await expect(page.getByText("Попыток доставки пока нет.")).toBeVisible()
  await expect(page.getByRole("button", { name: "Переотправить" })).toHaveCount(0)
  expect(mutationCount).toBe(0)
})

test("viewer cannot enumerate webhook registry through the UI", async ({ page }) => {
  await installPortalSession(page)
  await installCommonRoutes(page)
  let registryReads = 0

  await page.route("**/portal/v1/**", async (route) => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname === "/portal/v1/merchants") return json(route, merchant(portalRoles.viewer))
    if (pathname.includes("webhook-endpoints") || pathname === "/portal/v1/events") registryReads += 1
    return json(route, { object: "list", items: [], total: 0 })
  })

  await page.goto("/portal/developers/webhooks")
  await expect(page.getByText("Недостаточно прав")).toBeVisible()
  await expect(page.getByText(/Viewer остаётся в режиме без доступа/)).toBeVisible()
  expect(registryReads).toBe(0)
})

test("a stale step-up fails closed after one retry", async ({ page }) => {
  await installPortalSession(page)
  let rotateAttempts = 0
  const rotateKeys: string[] = []

  await page.route("**/api/v1/**", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === "/api/v1/auth/step-up") {
      return json(route, { expiresAt: "2026-08-30T12:00:01Z" })
    }
    return json(route, commonAPIResponse(pathname, portalOwner) ?? { items: [] })
  })
  await page.route("**/portal/v1/**", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === "/portal/v1/merchants") return json(route, merchant(portalRoles.owner))
    if (pathname === "/portal/v1/webhook-endpoints" && request.method() === "GET") {
      return json(route, { object: "list", items: [baseEndpoint], total: 1 })
    }
    if (pathname === "/portal/v1/events") return json(route, { object: "list", items: [], total: 0 })
    if (pathname === `/portal/v1/webhook-endpoints/${endpointId}/rotate-secret`) {
      rotateAttempts += 1
      rotateKeys.push(request.headers()["idempotency-key"] ?? "")
      return json(route, {
        code: "STEP_UP_REQUIRED",
        message: rotateAttempts === 1 ? "fresh object-bound step-up required" : "step-up expired before command lock",
        details: { actionCode: "portal.webhook_endpoints.rotate" },
      }, 403)
    }
    return json(route, { object: "list", items: [] })
  })

  await page.goto("/portal/developers/webhooks")
  await page.getByRole("button", { name: "Secret" }).click()
  const dialog = page.getByRole("dialog")
  await dialog.getByLabel("Пароль").fill("correct horse battery staple")
  await dialog.getByLabel("Код TOTP").fill("123456")
  await dialog.getByRole("button", { name: "Подтвердить" }).click()

  await expect(page.getByText("step-up expired before command lock", { exact: true })).toBeVisible()
  expect(rotateAttempts).toBe(2)
  expect(rotateKeys[0]).toMatch(/^[0-9a-f-]{36}$/i)
  expect(rotateKeys[1]).toBe(rotateKeys[0])
})

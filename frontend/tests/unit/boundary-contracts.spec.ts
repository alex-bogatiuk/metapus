import { test, expect } from "@playwright/test"
import { ApiError, api } from "../../lib/api"
import { automationRuleSchema } from "../../lib/schemas/automation"
import { batchProgressSchema, notificationSchema } from "../../lib/schemas/events"
import { mapRuleFromResponse, mapRuleToUpdate } from "../../lib/automation-rule-form"
import { jsonObjectSnapshot } from "../../lib/json-value"
import { fetchSSE } from "../../lib/sse-fetch"
import { useNotificationStore } from "../../stores/useNotificationStore"
import { webSocketMessageSchema } from "../../stores/useWebSocketStore"
import { ids } from "../helpers"
import type { BatchProgressEvent } from "../../types/common"
import type { JsonValue } from "../../lib/json-value"
import { invalidateResolveCache, resolveRefs, resolveSingleRef } from "../../lib/ref-resolver"
import { BatchActionByFilterRequestSchema, ResolveRefsRequestSchema } from "../../lib/generated/http-contract-schemas"
import { registerHTTPRoutes } from "../../lib/http-contracts"

test.describe.configure({ mode: "serial" })
const nativeFetch = globalThis.fetch
test.afterEach(() => { globalThis.fetch = nativeFetch })

test("catalog factory validates all reads and rejects malformed mutations before HTTP", async () => {
  const currency = { id: ids.token, code: "X", name: "Name", isoCode: null, symbol: null, decimalPlaces: 2, isBase: false, isFolder: false, deletionMark: false, version: 1 }
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json(currency) }
  expect(await api.currencies.get(ids.token)).toEqual(currency)
  await expect(api.currencies.update(ids.token, { name: "Name", version: NaN, decimalPlaces: 2, isBase: false })).rejects.toThrow()
  expect(calls).toBe(1)
  globalThis.fetch = async () => Response.json({ ...currency, decimalPlaces: "2" })
  await expect(api.currencies.get(ids.token)).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ items: [currency], totalCount: 1, hasMore: false, hasPrev: false })
  expect((await api.currencies.list()).items).toEqual([currency])
  globalThis.fetch = async () => Response.json({ items: [{ ...currency, version: null }], totalCount: 1, hasMore: false, hasPrev: false })
  await expect(api.currencies.list()).rejects.toThrow()
})

test("reference boundary deduplicates, chunks and validates every batch before caching", async () => {
  invalidateResolveCache()
  const refs = Array.from({ length: 101 }, (_, index) => ({ refType: "CryptoInvoice", refId: `10000000-0000-4000-8000-${String(index).padStart(12, "0")}` }))
  let requests = 0
  globalThis.fetch = async (_url, init) => {
    requests++
    const body = ResolveRefsRequestSchema.parse(JSON.parse(String(init?.body)))
    expect(body.refs.length).toBeLessThanOrEqual(100)
    return Response.json(body.refs.map(ref => ({ ...ref, presentation: "Document", entityType: "document", number: "N", date: "2026-09-14T00:00:00Z", posted: false, deletionMark: false,
      amount: requests === 2 ? 9007199254740993 : "9007199254740993", currencyId: ids.token, currencyCode: "USD", decimalPlaces: 2 })))
  }
  await expect(resolveRefs([...refs, ...refs])).rejects.toThrow()
  expect(requests).toBe(2)
  globalThis.fetch = async () => { requests++; return Response.json([]) }
  expect(await resolveSingleRef(refs[0])).toBeNull() // valid first chunk was not cached
  expect(requests).toBe(3)
  invalidateResolveCache()
})

test("reference transport failure rejects, while an authorized empty response remains a valid miss", async () => {
  invalidateResolveCache()
  const ref = { refType: "Currency", refId: ids.token }
  globalThis.fetch = async () => { throw new TypeError("network unavailable") }
  await expect(resolveSingleRef(ref)).rejects.toThrow("network unavailable")
  globalThis.fetch = async () => Response.json([], { status: 200 })
  expect(await resolveSingleRef(ref)).toBeNull()
})

test("search and preview reject malformed presentation fields before displaying them", async () => {
  globalThis.fetch = async () => Response.json({ query: "Name", results: [{ entityId: ids.token, title: { value: "unsafe" } }] })
  await expect(api.search.query("Name")).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ entityType: "document", entityKey: "crypto_invoice", entityName: "CryptoInvoice", title: "N", fields: [{ label: "Сумма", value: 9007199254740993 }], url: "/documents/crypto-invoices" })
  await expect(api.search.preview("document", "crypto_invoice", ids.token)).rejects.toThrow()
})

test("API errors validate their envelope before supplying UI messages and field details", () => {
  for (const body of ["null", "[]", "<html>Proxy error</html>", '{"message":{"secret":"value"}}', '{"code":4}', '{"message":"bad","details":[]}']) {
    const error = new ApiError(503, "Unavailable", body)
    expect(error.message).toBe("API 503: Unavailable")
    expect(error.parsedBody).toBeUndefined()
  }
  const error = new ApiError(400, "Bad Request", '{"code":"INVALID_REFERENCE","message":"Missing merchant","details":{"field":"merchantId"}}')
  expect(error.message).toBe("Missing merchant")
  expect(error.parsedBody?.details?.field).toBe("merchantId")
})

const ruleWire = {
  id: ids.wallet, name: "Rule", triggerType: "entity_event", eventType: "posted", targetEntities: [],
  reactionType: "notify", messageFormat: "text", actionTemplate: "Message", priority: 0, maxRetries: 0, cooldownSeconds: 0,
  isActive: false, executionCount: 0, errorCount: 0, deletionMark: false, version: 7,
  createdAt: "2026-09-12T00:00:00Z", updatedAt: "2026-09-12T00:00:00Z",
  subscribers: [{ id: ids.merchant, ruleId: ids.wallet, subscriberType: "doc_field", docFieldPath: "createdBy", deliveryMethod: "ui_notification", idx: 1 }],
}

test("automation API validates closed fields and preserves zero settings and document-field subscribers on save", async () => {
  globalThis.fetch = async () => Response.json(ruleWire)
  const rule = await api.automation.rules.get(ids.wallet)
  expect(rule.description).toBeNull() // omitted Go pointer
  const saved = mapRuleToUpdate(mapRuleFromResponse(rule))
  expect(saved).toMatchObject({ priority: 0, maxRetries: 0, isActive: false, version: 7 })
  expect(saved.subscribers).toEqual([{
    subscriberType: "doc_field", channelId: null, userId: null, roleName: null, docFieldPath: "createdBy", deliveryMethod: "ui_notification", idx: 1,
  }])
  for (const patch of [{ reactionType: "chain" }, { reactionType: "create_record" }, { priority: "0" }, { isActive: 1 }, { triggerType: "typo" }, { subscribers: [{ subscriberType: "typo" }] }]) {
    expect(automationRuleSchema.safeParse({ ...ruleWire, ...patch }).success).toBe(false)
    globalThis.fetch = async () => Response.json({ ...ruleWire, ...patch })
    await expect(api.automation.rules.get(ids.wallet)).rejects.toThrow()
  }
})

test("invalid websocket notifications never enter the notification store", () => {
  useNotificationStore.setState({ items: [], unreadCount: 0 })
  const invalid: JsonValue[] = [null, { id: ids.wallet }, { id: ids.wallet, title: { unsafe: true } }]
  for (const payload of invalid) {
    useNotificationStore.getState().handleWsMessage({ type: "notification", payload, time: 1 })
  }
  expect(useNotificationStore.getState().items).toEqual([])
  expect(useNotificationStore.getState().unreadCount).toBe(0)
  expect(webSocketMessageSchema.safeParse({ type: "notification", time: "1", payload: {} }).success).toBe(false)
  expect(notificationSchema.safeParse({}).success).toBe(false)
})

test("SSE validates split frames and omitted zero counters before callbacks", async () => {
  const bytes = new TextEncoder().encode([
    'data: {"type":"started","total":2}\n\n',
    'data: {"type":"progress","total":2,"processed":"1"}\n\n',
    'data: null\n\n',
    'data: {"type":"progress","total":2,"processed":1,"success":1}\n\n',
    'data: {"type":"completed","total":2,"processed":2,"success":2}',
  ].join(""))
  globalThis.fetch = async () => new Response(new ReadableStream({ start(controller) {
    for (let offset = 0; offset < bytes.length; offset += 7) controller.enqueue(bytes.slice(offset, offset + 7))
    controller.close()
  } }), { headers: { "Content-Type": "text/event-stream" } })
  const events: BatchProgressEvent[] = []
  registerHTTPRoutes([{ scope: "api", method: "POST", path: "/document/probe/batch-action-by-filter", request: BatchActionByFilterRequestSchema,
    requestFields: ["action", "filter"], response: null, optionalRequest: false }])
  await fetchSSE("/document/probe/batch-action-by-filter", { action: "post", filter: [] }, batchProgressSchema, event => events.push(event))
  expect(events).toEqual([
    { type: "started", total: 2, processed: 0, success: 0, failed: 0 },
    { type: "progress", total: 2, processed: 1, success: 1, failed: 0 },
    { type: "completed", total: 2, processed: 2, success: 2, failed: 0 },
  ])
})

test("JSON snapshots omit optional fields while preserving exact monetary strings", () => {
  expect(jsonObjectSnapshot({ amount: "9007199254740993", empty: undefined, nested: { optional: undefined } }))
    .toEqual({ amount: "9007199254740993", nested: {} })
  expect(() => jsonObjectSnapshot([1, 2])).toThrow()
  expect(() => jsonObjectSnapshot({ value: 1n })).toThrow()
})

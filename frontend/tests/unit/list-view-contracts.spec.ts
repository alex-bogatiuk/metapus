import { test, expect } from "@playwright/test"
import { api, readCachedListViews } from "../../lib/api"
import { transitionScope } from "../../lib/auth-scope"
import { listViewSchema, createListViewSchema } from "../../lib/schemas/list-views"
import { ids } from "../helpers"

const view = { id: ids.wallet, entityType: "crypto_payment", name: "Exact amounts", authorId: null,
  visibility: "shared", isDefault: false, sortOrder: 0, config: { filters: {} }, version: 1,
  createdAt: "2026-09-15T00:00:00Z", updatedAt: "2026-09-15T00:00:00Z" }
const config = { filters: { totalAmount: { operator: "gte" as const, value: "9007199254740993" } },
  columns: ["totalAmount"], sortColumn: null, sortDir: "asc" as const }
const nativeFetch = globalThis.fetch
test.describe.configure({ mode: "serial" })
test.beforeEach(() => {
  transitionScope(null)
  transitionScope({ tenantId: "list-view-contracts", principalId: ids.user, sessionId: ids.wallet, userVersion: "1", policyVersion: "1" })
})
test.afterEach(() => { globalThis.fetch = nativeFetch })

test("list views normalize actual Go omission but reject nulls and malformed nested filters", () => {
  expect(listViewSchema.parse(view).config).toEqual({ filters: {}, columns: [], sortColumn: null, sortDir: "asc" })
  for (const patch of [{ filters: null }, { columns: null }, { sortColumn: null }, { sortDir: "ascending" },
    { filters: { amount: { operator: "gte", value: { from: {} } } } }]) {
    expect(listViewSchema.safeParse({ ...view, config: { ...view.config, ...patch } }).success).toBe(false)
  }
})

test("an invalid later list item cannot populate cache and a subsequent read retries", async () => {
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json([view, { ...view, isDefault: "false" }]) }
  await expect(api.listViews.list(view.entityType)).rejects.toThrow()
  expect(readCachedListViews(view.entityType)).toBeUndefined()
  globalThis.fetch = async () => { calls++; return Response.json([view]) }
  expect(await api.listViews.list(view.entityType)).toHaveLength(1)
  await api.listViews.list(view.entityType)
  expect(calls).toBe(2)
})

test("list-view mutations validate before sending or caching and preserve exact money filters", async () => {
  globalThis.fetch = async () => Response.json([view])
  const previous = await api.listViews.list(view.entityType)
  let calls = 0
  let body: RequestInit["body"]
  globalThis.fetch = async (_url, options) => {
    calls++; body = options?.body
    return Response.json({ ...view, version: "2" })
  }
  const data = { entityType: view.entityType, name: view.name, visibility: "shared" as const, isDefault: false, config }
  const extraField = { ...data, extra: true }
  expect(createListViewSchema.safeParse(extraField).success).toBe(false)
  await expect(api.listViews.create(extraField)).rejects.toThrow("Unrecognized key")
  expect(calls).toBe(0)
  const invalidUpdate = { ...data, entityType: undefined, version: 1 }
  await expect(api.listViews.update(view.id, invalidUpdate)).rejects.toThrow()
  expect(calls).toBe(0)
  await expect(api.listViews.create(data)).rejects.toThrow()
  expect(typeof body === "string" ? JSON.parse(body) : body).toEqual(data)
  expect(readCachedListViews(view.entityType)).toEqual(previous)
  globalThis.fetch = async () => Response.json({ ...view, config: { ...config, sortColumn: undefined }, version: 2 })
  const { entityType, ...update } = data
  expect((await api.listViews.update(view.id, { ...update, version: 1 })).config).toEqual(config)
  expect(readCachedListViews(entityType)?.[0].version).toBe(2)
})

test("a response from an old scope cannot restore cleared list views", async () => {
  let finish: (response: Response) => void = () => { throw new Error("request not started") }
  globalThis.fetch = () => new Promise<Response>(resolve => { finish = resolve })
  const pending = api.listViews.list(view.entityType)
  transitionScope(null)
  finish(Response.json([view]))
  await expect(pending).rejects.toThrow("Session scope changed")
  expect(readCachedListViews(view.entityType)).toBeUndefined()
})

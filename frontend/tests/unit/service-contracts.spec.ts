import { expect, test } from "@playwright/test"
import { api, apiFetch, apiFetchBlob } from "../../lib/api"
import { findHTTPContract, validateHTTPBody } from "../../lib/http-contracts"
import { ids } from "../helpers"

const nativeFetch = globalThis.fetch
test.describe.configure({ mode: "serial" })
test.afterEach(() => { globalThis.fetch = nativeFetch })

test("automation boundaries normalize omitted nullable fields and reject invalid account kinds", async () => {
  const account = {
    id: ids.token, name: "Sender", accountType: "email", config: {}, isActive: false, status: "disabled",
    channelCount: 0, deletionMark: false, version: 1, createdAt: "2026-09-20T00:00:00Z", updatedAt: "2026-09-20T00:00:00Z",
  }
  globalThis.fetch = async () => Response.json(account)
  expect(await api.automation.accounts.get(ids.token)).toMatchObject({ organizationId: null, lastError: null, lastSuccessAt: null, isActive: false })
  globalThis.fetch = async () => Response.json({ ...account, accountType: "misspelled" })
  await expect(api.automation.accounts.get(ids.token)).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ ...account, channelCount: "0" })
  await expect(api.automation.accounts.get(ids.token)).rejects.toThrow()
})

test("closed automation and settings requests reject unintended properties before HTTP", async () => {
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json({ id: ids.token }) }
  const channel = { name: "Destination", accountId: ids.token, destination: { chatId: "123" }, isActive: false }
  const invalidChannel = { ...channel, secret: "accidental" }
  const invalidSettings = { batchConcurrency: 2, injected: 1 }
  await expect(api.automation.channels.create(invalidChannel)).rejects.toThrow("Unrecognized key")
  await expect(api.settings.updateSection("performance", invalidSettings, 1)).rejects.toThrow("Unrecognized key")
  expect(calls).toBe(0)
  expect(await api.automation.channels.create(channel)).toEqual({ id: ids.token })
  expect(calls).toBe(1)
})

test("observability lists and statistics reject malformed numbers before reaching widgets", async () => {
  globalThis.fetch = async () => Response.json({ items: [], hasMore: false, totalCount: "10" })
  await expect(api.system.workerJobs.list()).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ total: 0, info: 0, warning: 0, error: 0, critical: false })
  await expect(api.system.eventLog.stats()).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ items: [], unreadCount: 0 })
  expect(await api.system.notifications.list()).toEqual({ items: [], unreadCount: 0 })
})

test("unregistered calls and invalid export bodies stop before HTTP", async () => {
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json({}) }
  await expect(apiFetch("/unregistered")).rejects.toThrow("No HTTP contract")
  await expect(apiFetchBlob("/reports/document-journal/export", { method: "POST", body: JSON.stringify({ dataset: "document-journal", injected: 1 }) })).rejects.toThrow("Unrecognized key")
  expect(calls).toBe(0)
})

test("key-scoped network policies and bodyless logout have their own contracts", async () => {
  for (const method of ["POST", "DELETE"]) {
    const path = `/api-keys/${ids.token}/allowed-networks${method === "DELETE" ? `/${ids.wallet}` : ""}`
    const contract = findHTTPContract("portal", method, path)
    expect(contract?.response).not.toBeNull()
    if (!contract) throw new Error("Network policy contract is missing")
    expect(() => validateHTTPBody(contract, JSON.stringify({ reason: "Approved", injected: 1 }))).toThrow()
  }
  globalThis.fetch = async () => new Response(null, { status: 204 })
  await expect(api.auth.logout()).resolves.toBeUndefined()
})

test("provider action availability is required and malformed grants cannot reach AutoForm", async () => {
  globalThis.fetch = async () => Response.json({ id: ids.token, version: 1 })
  await expect(apiFetch(`/document/crypto-provider-balance-operation/${ids.token}`)).rejects.toThrow()
})

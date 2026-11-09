import { expect, test } from "@playwright/test"
import { apiFetch } from "../../lib/api"
import { projectFormRequest, findHTTPContract } from "../../lib/http-contracts"
import { tokenRecord, ids } from "../helpers"

const nativeFetch = globalThis.fetch
test.describe.configure({ mode: "serial" })
test.afterEach(() => { globalThis.fetch = nativeFetch })

test("form projection uses writable DTO fields, preserves exact strings and requires complete values", () => {
  const path = `/catalog/tokens/${ids.token}`
  const draft = { ...tokenRecord, sweepThreshold: "9007199254740993", network: { id: ids.network, name: "Network" }, createdAt: "server", injected: true }
  const payload = projectFormRequest("PUT", path, draft)
  expect(payload.sweepThreshold).toBe("9007199254740993")
  expect(payload.version).toBe(1)
  for (const key of ["id", "network", "createdAt", "injected", "deletionMark"]) expect(payload).not.toHaveProperty(key)
  expect(() => projectFormRequest("PUT", path, { name: "Incomplete", version: 1 })).toThrow()
  expect(() => projectFormRequest("PUT", "/catalog/unregistered/id", draft)).toThrow("No form request contract")
  const networkOperation = findHTTPContract("api", "PUT", `/document/crypto-network-operation/${ids.outflow}/repost`)
  expect(networkOperation?.requestFields).toEqual(["sourceWalletId", "costTokenId", "costAmount", "description", "version"])
})

test("raw AutoForm and generic CRUD HTTP calls share concrete response and request validation", async () => {
  const path = `/catalog/tokens/${ids.token}`
  globalThis.fetch = async () => Response.json({ id: ids.token, name: "Partial", version: 1 })
  await expect(apiFetch(path)).rejects.toThrow()
  globalThis.fetch = async () => Response.json(tokenRecord)
  expect(await apiFetch(path)).toEqual(tokenRecord)
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json(tokenRecord) }
  await expect(apiFetch(path, { method: "PUT", body: JSON.stringify(tokenRecord) })).rejects.toThrow("Unrecognized key")
  expect(calls).toBe(0)
  await apiFetch(path, { method: "PUT", body: JSON.stringify(projectFormRequest("PUT", path, tokenRecord)) })
  expect(calls).toBe(1)
})

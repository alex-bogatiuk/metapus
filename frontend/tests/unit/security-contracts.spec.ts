import { test, expect } from "@playwright/test"
import { api } from "../../lib/api"
import { ids } from "../helpers"

const nativeFetch = globalThis.fetch
test.describe.configure({ mode: "serial" })
test.afterEach(() => { globalThis.fetch = nativeFetch })
const rule = { id: ids.token, profileId: ids.wallet, name: "Rule", entityName: "Currency", actions: ["read"], expression: "true", effect: "deny", priority: 0, enabled: false,
  createdAt: "2026-09-14T10:00:00Z", updatedAt: "2026-09-14T10:00:00Z" }
const profile = { id: ids.wallet, code: "TEST", name: "Profile", isSystem: false, createdAt: rule.createdAt, updatedAt: rule.updatedAt, userCount: 0,
  dimensions: { organization: [] }, fieldPolicies: [{ entityName: "Currency", action: "read", allowedFields: [] }], policyRules: [rule] }

test("security boundaries preserve empty denied dimensions, zero priorities and disabled rules", async () => {
  globalThis.fetch = async () => Response.json({ items: [profile] })
  expect((await api.security.profiles.list()).items[0]).toEqual(profile)
  let body = ""
  globalThis.fetch = async (_url, init) => { body = String(init?.body); return Response.json(rule) }
  expect(await api.security.rules.update(ids.wallet, ids.token, { priority: 0, enabled: false })).toEqual(rule)
  expect(JSON.parse(body)).toEqual({ priority: 0, enabled: false })
})

test("invalid rights and CEL results are rejected before editing or publishing them", async () => {
  for (const value of [{ ...profile, dimensions: { organization: [123] } },
    { ...profile, fieldPolicies: [{ entityName: "Currency", action: "read", allowedFields: null }] },
    { ...profile, policyRules: [{ ...rule, effect: "unknown" }] }]) {
    globalThis.fetch = async () => Response.json(value)
    await expect(api.security.profiles.get(ids.wallet)).rejects.toThrow()
  }
  let called = false
  globalThis.fetch = async () => { called = true; return Response.json(rule) }
  await expect(api.security.rules.update(ids.wallet, ids.token, { effect: "unknown" })).rejects.toThrow()
  expect(called).toBe(false)
  globalThis.fetch = async () => Response.json({ valid: "true" })
  await expect(api.security.rules.validate("true")).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ result: "false", elapsed: "1ms" })
  await expect(api.security.rules.test("true")).rejects.toThrow()
})

test("security audit list uses its DTO envelope and keeps the limit in the URL", async () => {
  let url = ""
  globalThis.fetch = async input => { url = String(input); return Response.json({ items: [{ id: ids.token, action: "update", createdAt: rule.createdAt, changes: { permissions: [] } }] }) }
  expect((await api.security.profiles.auditHistory(ids.wallet, 7)).items[0].changes).toEqual({ permissions: [] })
  expect(url).toContain(`/security/profiles/${ids.wallet}/audit?limit=7`)
})

test("refined security request contracts reject extra root and nested fields before HTTP", async () => {
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json(profile) }
  const extraRule = { priority: 0, enabled: false, elevated: true }
  await expect(api.security.rules.update(ids.wallet, ids.token, extraRule)).rejects.toThrow("Unrecognized key")
  const extraProfile = { name: "Profile", elevated: true }
  await expect(api.security.profiles.update(ids.wallet, extraProfile)).rejects.toThrow("Unrecognized key")
  const nestedExtra = { fieldPolicies: [{ entityName: "Currency", action: "read", allowedFields: [], elevated: true }] }
  await expect(api.security.profiles.update(ids.wallet, nestedExtra)).rejects.toThrow("Unrecognized key")
  expect(calls).toBe(0)
})

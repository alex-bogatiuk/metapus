import { test, expect } from "@playwright/test"
import { api } from "../../lib/api"
import { authLoginResponseSchema, loginResponseSchema } from "../../lib/schemas/auth"
import { erpUser, ids } from "../helpers"

const nativeFetch = globalThis.fetch
test.describe.configure({ mode: "serial" })
test.afterEach(() => { globalThis.fetch = nativeFetch })
const tokens = { accessToken: "access", expiresAt: "2099-01-01T00:00:00Z", tokenType: "Bearer",
  cacheScope: { tenantId: "tenant", principalId: ids.user, sessionId: ids.wallet, userVersion: "9007199254740993", policyVersion: "1" } }
const challenge = { status: "mfa_required", challengeId: ids.token, expiresAt: "2026-09-14T10:00:00Z" }

test("login responses distinguish a complete session from MFA without accepting mixed or unknown states", () => {
  expect(authLoginResponseSchema.parse({ tokens, user: erpUser })).toEqual({ tokens, user: erpUser })
  expect(authLoginResponseSchema.parse(challenge)).toEqual(challenge)
  for (const value of [{ tokens: null, user: erpUser }, { tokens, user: null }, { tokens, user: erpUser, ...challenge },
    { ...challenge, status: "unknown" }, { ...challenge, tokens }, { tokens: { ...tokens, cacheScope: { ...tokens.cacheScope, userVersion: 9007199254740993 } }, user: erpUser }]) {
    expect(authLoginResponseSchema.safeParse(value).success).toBe(false)
  }
})
test("real backend challenge response passes authLoginResponseSchema", () => {
  const realChallengeResponse = {
    status: "mfa_required",
    challengeId: "01a0afb6-db3d-7e64-9393-0fc839d7f34e",
    expiresAt: "2026-09-17T14:23:11.6139433Z",
    user: {
      id: "01a0af70-168a-7778-89f4-882cb4fe95d4",
      email: "admin@metapus.io",
      firstName: "System",
      lastName: "Admin",
      fullName: "System Admin",
      isActive: true,
      isAdmin: true,
      emailVerified: true,
      roles: [
        {
          id: "b0000000-0000-0000-0000-000000000001",
          code: "admin",
          name: "Администратор",
          description: "Full access to all features",
          isSystem: true,
        },
      ],
      mfaEnabled: true,
      mfaPending: false,
      mfaRequired: true,
      mfaGraceEndsAt: "2026-09-17T16:45:38.407186+03:00",
      createdAt: "0001-01-01T00:00:00Z",
    },
  }
  const result = authLoginResponseSchema.safeParse(realChallengeResponse)
  if (!result.success) {
    console.error("CHALLENGE FAILED PARSE:", JSON.stringify(result.error.format(), null, 2))
  }
  expect(result.success).toBe(true)
})

test("real backend verifyMFA response passes loginResponseSchema", () => {
  const realVerifyResponse = {
    tokens: {
      cacheScope: {
        tenantId: "01a0af70-0892-77d4-aedb-3b30a9a583f2",
        principalId: "01a0af70-168a-7778-89f4-882cb4fe95d4",
        sessionId: "01a0afaa-0541-7327-95b4-88719638e5c0",
        userVersion: "1",
        policyVersion: "1",
      },
      accessToken: "eyJhbGciOi...",
      expiresAt: "2026-09-17T14:29:10.401206Z",
      tokenType: "Bearer",
    },
    user: {
      id: "01a0af70-168a-7778-89f4-882cb4fe95d4",
      email: "admin@metapus.io",
      firstName: "System",
      lastName: "Admin",
      fullName: "System Admin",
      isActive: true,
      isAdmin: true,
      emailVerified: true,
      roles: [
        {
          id: "b0000000-0000-0000-0000-000000000001",
          code: "admin",
          name: "Администратор",
          description: "Full access to all features",
          isSystem: true,
        },
      ],
      accessScopes: { merchant: ["01a0af70-1743-7c7a-adf6-9ffe05dcaf98"] },
      mfaEnabled: true,
      mfaPending: false,
      mfaRequired: true,
      mfaGraceEndsAt: "2026-09-17T16:45:38.407186+03:00",
      mfaVerifiedAt: "2026-09-17T13:59:10.401206Z",
      createdAt: "0001-01-01T00:00:00Z",
    },
  }
  const result = loginResponseSchema.safeParse(realVerifyResponse)
  if (!result.success) {
    console.error("FAILED PARSE:", JSON.stringify(result.error.format(), null, 2))
  }
  expect(result.success).toBe(true)
})

test("auth HTTP boundaries validate request DTOs and responses before consumers use them", async () => {
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json({ tokens, user: erpUser }) }
  const badRequest = { email: "user@example.test", password: "password", unexpected: true }
  await expect(api.auth.login(badRequest)).rejects.toThrow()
  expect(calls).toBe(0)
  expect(await api.auth.login({ email: "user@example.test", password: "password" })).toEqual({ tokens, user: erpUser })
  globalThis.fetch = async () => Response.json({ ...erpUser, isAdmin: "true" })
  await expect(api.auth.me()).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ tokens: null, user: erpUser })
  await expect(api.auth.verifyMFA({ challengeId: ids.token, code: "123456" })).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ expiresAt: 123 })
  await expect(api.auth.stepUp({ actionCode: "test", password: "password", code: "123456" })).rejects.toThrow()
})

test("admin lists, effective access, permissions and WebSocket ticket reject malformed responses", async () => {
  globalThis.fetch = async () => Response.json({ items: [null], total: 1 })
  for (const read of [() => api.users.list(), () => api.roles.list(), () => api.permissions.list(), () => api.roles.getPermissions(ids.wallet)]) {
    await expect(read()).rejects.toThrow()
  }
  globalThis.fetch = async () => Response.json({ user: null, permissions: [] })
  await expect(api.users.effectiveAccess(ids.user)).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ items: [false] })
  await expect(api.auth.permissions()).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ ticket: null })
  await expect(api.auth.webSocketTicket()).rejects.toThrow()
  globalThis.fetch = async () => Response.json({ items: [], total: 0 })
  expect(await api.users.list()).toEqual({ items: [], total: 0 })
  expect(await api.roles.list()).toEqual({ items: [] })
})

test("admin updates preserve explicit false and null and reject extra fields before HTTP", async () => {
  let calls = 0
  let body: RequestInit["body"]
  globalThis.fetch = async (_url, options) => { calls++; body = options?.body; return Response.json(erpUser) }
  const patch = { isActive: false, isAdmin: false, firstName: null }
  await api.users.update(ids.user, patch)
  expect(typeof body === "string" ? JSON.parse(body) : body).toEqual(patch)
  const invalid = { ...patch, permissionCodes: ["*"] }
  await expect(api.users.update(ids.user, invalid)).rejects.toThrow()
  expect(calls).toBe(1)
})

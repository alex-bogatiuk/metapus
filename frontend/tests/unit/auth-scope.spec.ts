import { test, expect } from "@playwright/test"
import { useAuthStore } from "../../stores/useAuthStore"
import { useFormDraftStore, createFormDraftKey } from "../../stores/useFormDraftStore"
import { useTabStateStore, createTabStateKey } from "../../stores/useTabStateStore"
import { useNotificationStore } from "../../stores/useNotificationStore"
import { useMetadataStore } from "../../stores/useMetadataStore"
import { api, apiFetch, portalFetch, apiFetchBlob, bootstrapAccessToken, ApiError } from "../../lib/api"
import { getSessionGeneration, scopeKey } from "../../lib/auth-scope"
import { erpUser, ids } from "../helpers"
import type { TokenResponse } from "../../types/auth"

test.describe.configure({ mode: "serial" })
const previewKey = createTabStateKey<string>("test.preview")
const draftKey = createFormDraftKey<{ secret: string }>("test.draft")
const userB = { ...erpUser, id: ids.merchant, fullName: "User B" }
function tokens(user = erpUser, policyVersion = "1", sessionId = ids.wallet): TokenResponse {
  return { accessToken: "token-" + user.id, expiresAt: "2099-01-01T00:00:00Z", tokenType: "Bearer",
    cacheScope: { tenantId: "tenant-a", principalId: user.id, sessionId, userVersion: "1", policyVersion } }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(r => { resolve = r })
  return { promise, resolve }
}
const nativeFetch = globalThis.fetch
test.afterEach(() => { globalThis.fetch = nativeFetch; useAuthStore.getState().logout() })

test("identity, tenant, session and rights transitions clear drafts and caches; rotation preserves edits", () => {
  const auth = useAuthStore.getState()
  auth.setAuth(tokens(), erpUser)
  const generation = getSessionGeneration()
  useFormDraftStore.getState().saveDraft("/A", draftKey, { secret: "draft A" })
  useTabStateStore.getState().set("/A", previewKey, "name A")
  auth.setTokens({ ...tokens(), accessToken: "rotated" })
  expect(getSessionGeneration()).toBe(generation)
  expect(useFormDraftStore.getState().loadDraft("/A", draftKey)?.secret).toBe("draft A")
  auth.setTokens(tokens(erpUser, "2"))
  expect(getSessionGeneration()).toBeGreaterThan(generation)
  expect(useFormDraftStore.getState().drafts).toEqual({})
  expect(useTabStateStore.getState().cache).toEqual({})
  expect(useAuthStore.getState().user).toBeNull() // reauthorize the user view before the shell
  auth.setUser(erpUser)
  useFormDraftStore.getState().saveDraft("/A", draftKey, { secret: "new edit" })
  auth.startImpersonation(tokens(userB), userB)
  expect(useFormDraftStore.getState().drafts).toEqual({})
  expect(useAuthStore.getState().user?.id).toBe(userB.id)
  auth.stopImpersonation()
  expect(useAuthStore.getState().user?.id).toBe(erpUser.id)
  const old = getSessionGeneration()
  auth.setAuth({ ...tokens(), cacheScope: { ...tokens().cacheScope, tenantId: "tenant-b" } }, erpUser)
  expect(getSessionGeneration()).toBeGreaterThan(old)
  expect(() => auth.setTokens(tokens(userB))).toThrow("does not match")
})

test("late JSON, portal and blob responses cannot complete in the next identity", async () => {
  for (const fetcher of [() => apiFetch("/auth/me"), () => portalFetch("/merchants"), () => apiFetchBlob(`/document/crypto-invoice/${ids.token}/print`)]) {
    useAuthStore.getState().setAuth(tokens(), erpUser)
    const delayed = deferred<Response>()
    let requestSignal: AbortSignal | null | undefined
    globalThis.fetch = async (_url, options) => { requestSignal = options?.signal; return delayed.promise }
    const pending = fetcher()
    const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" })
    useAuthStore.getState().logout()
    useAuthStore.getState().setAuth(tokens(userB), userB)
    expect(requestSignal?.aborted).toBe(true)
    delayed.resolve(Response.json({ name: "secret A" }))
    await rejected
    expect(useAuthStore.getState().user?.id).toBe(userB.id)
  }
})

test("JSON body delayed after headers and old refresh failures cannot overwrite or log out B", async () => {
  useAuthStore.getState().setAuth(tokens(), erpUser)
  let body!: ReadableStreamDefaultController<Uint8Array>
  const started = deferred<void>()
  globalThis.fetch = async () => {
    const response = new Response(new ReadableStream<Uint8Array>({ start(controller) { body = controller } }))
    started.resolve()
    return response
  }
  const pending = apiFetch("/auth/me")
  const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" })
  await started.promise
  useAuthStore.getState().setAuth(tokens(userB), userB)
  body.enqueue(new TextEncoder().encode('{"secret":"A"}')); body.close()
  await rejected
  for (const response of [Response.json(tokens(), { status: 200 }), Response.json({}, { status: 401 })]) {
    useAuthStore.getState().setAuth(tokens(), erpUser)
    const delayed = deferred<Response>()
    globalThis.fetch = async () => delayed.promise
    const refresh = bootstrapAccessToken()
    useAuthStore.getState().setAuth(tokens(userB), userB)
    delayed.resolve(response)
    await refresh
    expect(useAuthStore.getState().tokens?.accessToken).toBe(tokens(userB).accessToken)
  }
})

test("optimistic rollback and metadata rejection from A cannot restore A state in B", async () => {
  useAuthStore.getState().setAuth(tokens(), erpUser)
  useNotificationStore.setState({ items: [{ id: ids.outflow, userId: erpUser.id, title: "Secret A", message: "Body A", severity: "info", isRead: false, version: 1, deletionMark: false, createdAt: erpUser.createdAt, updatedAt: erpUser.createdAt }], unreadCount: 1 })
  const delayed = deferred<Response>()
  globalThis.fetch = async () => delayed.promise
  const rollback = useNotificationStore.getState().markAsRead(ids.outflow)
  const metadata = useMetadataStore.getState().fetch()
  useAuthStore.getState().setAuth(tokens(userB), userB)
  delayed.resolve(Response.json({}, { status: 500 }))
  await Promise.all([rollback, metadata])
  expect(useNotificationStore.getState().items).toEqual([])
  expect(useMetadataStore.getState().entities).toEqual([])
})

test("metadata caches are refetched after scope change and exact rights versions differ", async () => {
  let calls = 0
  globalThis.fetch = async () => { calls++; return Response.json({ key: "currency", name: "Currency", type: "catalog", presentation: { singular: "Currency", plural: "Currencies" }, fields: [] }) }
  useAuthStore.getState().setAuth(tokens(), erpUser)
  await api.meta.getEntity("Currency")
  await api.meta.getEntity("Currency")
  expect(calls).toBe(1)
  useAuthStore.getState().setAuth(tokens(userB), userB)
  await api.meta.getEntity("Currency")
  expect(calls).toBe(2)
  expect(scopeKey(tokens(erpUser, "9007199254740992").cacheScope)).not.toBe(scopeKey(tokens(erpUser, "9007199254740993").cacheScope))
})

test("refresh rate limiting and temporary server failures preserve the active session", async () => {
  for (const status of [429, 500, 503]) {
    useAuthStore.getState().setAuth(tokens(), erpUser)
    const generation = getSessionGeneration()
    globalThis.fetch = async () => Response.json({ message: "Try again later" }, { status })
    await expect(bootstrapAccessToken()).rejects.toBeInstanceOf(ApiError)
    expect(useAuthStore.getState().isAuthenticated).toBe(true)
    expect(getSessionGeneration()).toBe(generation)
  }
  globalThis.fetch = async () => { throw new TypeError("Network unavailable") }
  await expect(bootstrapAccessToken()).rejects.toThrow("Network unavailable")
  expect(useAuthStore.getState().isAuthenticated).toBe(true)
})

test("invalid refresh credentials still terminate the current session", async () => {
  for (const status of [401, 403]) {
    useAuthStore.getState().setAuth(tokens(), erpUser)
    globalThis.fetch = async () => Response.json({ message: "Session expired" }, { status })
    expect(await bootstrapAccessToken()).toBeNull()
    expect(useAuthStore.getState().isAuthenticated).toBe(false)
    expect(useAuthStore.getState().tokens).toBeNull()
  }
})

test("malformed refresh responses and unexpected identities still fail closed", async () => {
  for (const body of [{ accessToken: "invalid" }, tokens(userB), tokens(erpUser, "1", ids.token)]) {
    useAuthStore.getState().setAuth(tokens(), erpUser)
    globalThis.fetch = async () => Response.json(body)
    expect(await bootstrapAccessToken()).toBeNull()
    expect(useAuthStore.getState().isAuthenticated).toBe(false)
  }
})

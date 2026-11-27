import { z } from "zod"
import { requireHTTPContract, validateHTTPBody } from "@/lib/http-contracts"
import { jsonObjectSchema } from "@/lib/json-value"
import { assertCurrentSession, getSessionGeneration, isCurrentSession, onScopeReset, sessionSignal } from "@/lib/auth-scope"
import { tokenResponseSchema } from "@/lib/schemas/auth"
import { useAuthStore } from "@/stores/useAuthStore"
import { requestStepUp } from "@/lib/step-up"
import type { TokenResponse } from "@/types/auth"
import type { CursorListResponse, CursorListParams } from "@/types/common"
import type { SetDeletionMarkRequest } from "@/types/catalog"

export const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"
export const TENANT_ID = process.env.NEXT_PUBLIC_TENANT_ID ?? ""
export const UUID_PATH_PARAM_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function assertUuidPathParam(value: string, name: string): string {
    if (!UUID_PATH_PARAM_RE.test(value)) {
        throw new Error(`${name} must be a UUID`)
    }
    return value
}

// ── Generic fetcher error ────────────────────────────────────────────────

const apiErrorBodySchema = z.object({
    code: z.string().optional(),
    message: z.string().optional(),
    details: jsonObjectSchema.nullish().transform(value => value ?? undefined),
})
export type ApiErrorBody = z.infer<typeof apiErrorBodySchema>

export class ApiError extends Error {
    public readonly parsedBody?: ApiErrorBody

    constructor(
        public status: number,
        public statusText: string,
        public body?: string
    ) {
        let parsed: ApiErrorBody | undefined
        if (typeof body === "string" && body) {
            try {
                const result = apiErrorBodySchema.safeParse(JSON.parse(body))
                if (result.success) parsed = result.data
            } catch { /* Non-JSON proxy errors use the HTTP status below. */ }
        }
        super(parsed?.message ?? `API ${status}: ${statusText}`)
        this.name = "ApiError"
        this.parsedBody = parsed
    }
}

// ── Token refresh mutex ─────────────────────────────────────────────────
let refreshPromise: Promise<TokenResponse | null> | null = null
const AUTH_REFRESH_LOCK = "metapus-auth-refresh"

async function withCrossTabRefreshLock<T>(operation: () => Promise<T>): Promise<T> {
    if (typeof navigator === "undefined" || !("locks" in navigator)) {
        return operation()
    }

    return navigator.locks.request(AUTH_REFRESH_LOCK, operation)
}

async function doRefreshToken(generation: number): Promise<TokenResponse | null> {
    if (!isCurrentSession(generation)) return null
    const { setTokens, logout } = useAuthStore.getState()
    let res: Response
    try {
        res = await fetch(`${API_BASE}/auth/refresh`, {
            signal: sessionSignal(),
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                ...((useAuthStore.getState().cacheScope?.tenantId ?? TENANT_ID) ? { "X-Tenant-ID": useAuthStore.getState().cacheScope?.tenantId ?? TENANT_ID } : {}),
            },
            credentials: "include",
            body: JSON.stringify({}),
        })
        assertCurrentSession(generation)
    } catch (error) {
        if (!isCurrentSession(generation)) return null
        throw error
    }

    if (!res.ok) {
        // A rate limit or outage does not invalidate the refresh cookie. Keep
        // the session so bootstrap can retry without logging every tab out.
        if (res.status === 401 || res.status === 403) {
            logout()
            return null
        }
        const error = await errorFromResponse(res)
        if (!isCurrentSession(generation)) return null
        throw error
    }

    try {
        const newTokens = tokenResponseSchema.parse(await res.json())
        assertCurrentSession(generation)
        setTokens(newTokens)
        return newTokens
    } catch {
        // Invalid contracts and unexpected identities must still fail closed.
        if (isCurrentSession(generation)) logout()
        return null
    }
}

export function refreshAccessToken(): Promise<TokenResponse | null> {
    if (!refreshPromise) {
        const generation = getSessionGeneration()
        const previousToken = useAuthStore.getState().tokens?.accessToken
        const pending = withCrossTabRefreshLock(() => {
            if (!isCurrentSession(generation)) return Promise.resolve(null)
            const currentTokens = useAuthStore.getState().tokens
            // A sign-in in this tab may have supplied a new token while it
            // waited for the shared cookie lock.
            if (currentTokens && currentTokens.accessToken !== previousToken) return Promise.resolve(currentTokens)
            return doRefreshToken(generation)
        })
            .finally(() => {
                if (refreshPromise === pending) refreshPromise = null
            })
        refreshPromise = pending
    }
    return refreshPromise
}

export function bootstrapAccessToken(): Promise<TokenResponse | null> {
    return refreshAccessToken()
}

// ── Paths that should never trigger a refresh retry ─────────────────────
const NO_RETRY_PATHS = [
    "/auth/login",
    "/auth/register",
    "/auth/refresh",
    "/auth/mfa/verify",
    "/auth/mfa/enroll/start",
    "/auth/mfa/enroll/confirm",
    "/auth/step-up",
]

async function errorFromResponse(res: Response): Promise<ApiError> {
    const body = await res.text().catch(() => undefined)
    return new ApiError(res.status, res.statusText, body)
}

function shouldRetryWithRefresh(path: string, err: ApiError): boolean {
    if (err.status !== 401 || NO_RETRY_PATHS.includes(path)) return false
    return err.parsedBody?.code !== "SESSION_REVOKED"
}

function logoutIfSessionRevoked(err: ApiError): void {
    if (err.status === 401 && err.parsedBody?.code === "SESSION_REVOKED") {
        useAuthStore.getState().logout()
    }
}

function stepUpActionCode(err: ApiError): string | null {
    if (err.parsedBody?.code !== "STEP_UP_REQUIRED") return null
    const actionCode = err.parsedBody.details?.actionCode
    return typeof actionCode === "string" && actionCode ? actionCode : null
}

async function resolveSecurityChallenge(err: ApiError): Promise<boolean> {
    const actionCode = stepUpActionCode(err)
    const enrollmentRequired = err.parsedBody?.code === "MFA_ENROLLMENT_REQUIRED"
    if (!actionCode && !enrollmentRequired) return false
    try {
        await requestStepUp(actionCode ?? "auth.session_mfa.enroll")
        return true
    } catch {
        return false
    }
}

function buildHeaders(optHeaders?: HeadersInit): Record<string, string> {
    const authHeaders: Record<string, string> = {}
    const tokens = useAuthStore.getState().tokens
    if (tokens?.accessToken) {
        authHeaders["Authorization"] = `${tokens.tokenType || "Bearer"} ${tokens.accessToken}`
    }

    const tenantHeaders: Record<string, string> = {}
    const tenantId = useAuthStore.getState().cacheScope?.tenantId ?? TENANT_ID
    if (tenantId) {
        tenantHeaders["X-Tenant-ID"] = tenantId
    }

    const suppliedHeaders = optHeaders
        ? Object.fromEntries(new Headers(optHeaders).entries())
        : {}

    return {
        "Content-Type": "application/json",
        ...tenantHeaders,
        ...authHeaders,
        ...suppliedHeaders,
    }
}

function makeIdempotencyKey(): string {
    if (typeof globalThis.crypto?.randomUUID === "function") {
        return globalThis.crypto.randomUUID()
    }
    return `idem-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export function idempotencyHeaders(key?: string): Record<string, string> {
    return { "Idempotency-Key": key || makeIdempotencyKey() }
}

const pendingMutationKeys = new Map<string, string>()
onScopeReset(() => { pendingMutationKeys.clear(); refreshPromise = null })

export function shouldRetainMutationKey<T>(err: T): boolean {
    return !(err instanceof ApiError) || err.status >= 500 || err.status === 429 ||
        (err.status === 409 && err.parsedBody?.code === "IDEMPOTENCY_IN_PROGRESS")
}

function withStableMutationKey(scope: string, path: string, options?: RequestInit): {
    options: RequestInit
    pendingIdentity?: string
    key?: string
} {
    const method = (options?.method ?? "GET").toUpperCase()
    if (!["POST", "PUT", "PATCH", "DELETE"].includes(method)) {
        return { options: options ?? {} }
    }
    const headers = new Headers(options?.headers)
    const suppliedKey = headers.get("Idempotency-Key")
    if (suppliedKey) {
        return { options: { ...options, headers } }
    }
    const body = typeof options?.body === "string" ? options.body : ""
    const pendingIdentity = `${scope}:${method}:${path}:${body}`
    const key = pendingMutationKeys.get(pendingIdentity) ?? makeIdempotencyKey()
    pendingMutationKeys.set(pendingIdentity, key)
    headers.set("Idempotency-Key", key)
    return { options: { ...options, headers }, pendingIdentity, key }
}

async function requestResponse(base: string, path: string, options: RequestInit, stepUpRetried: boolean, generation: number): Promise<Response> {
    assertCurrentSession(generation)
    const { headers, signal, ...rest } = options
    const requestSignal = signal ? AbortSignal.any([signal, sessionSignal()]) : sessionSignal()
    const send = async () => {
        assertCurrentSession(generation)
        const result = await fetch(base + path, { ...rest, signal: requestSignal, credentials: "include", cache: "no-store", headers: buildHeaders(headers) })
        assertCurrentSession(generation)
        return result
    }
    let res = await send()
    if (res.status === 401) {
        const err = await errorFromResponse(res)
        assertCurrentSession(generation)
        if (!shouldRetryWithRefresh(path, err)) {
            logoutIfSessionRevoked(err)
            throw err
        }
        const tokens = await refreshAccessToken()
        assertCurrentSession(generation)
        if (!tokens) throw err
        res = await send()
    }
    if (!res.ok) {
        const err = await errorFromResponse(res)
        assertCurrentSession(generation)
        const retry = !stepUpRetried && await resolveSecurityChallenge(err)
        assertCurrentSession(generation)
        if (retry) return requestResponse(base, path, options, true, generation)
        logoutIfSessionRevoked(err)
        throw err
    }
    return res
}

/**
 * Every JSON exchange is checked against its route contract: the body before
 * it is sent, the response before it is returned. A caller's `decode` schema
 * replaces the contract's response check when it is the same schema, so no
 * response is validated twice.
 */
async function scopedJSON<T>(scope: string, base: string, path: string, options?: RequestInit, stepUpRetried = false, decode?: z.ZodType<T>): Promise<T> {
    const generation = getSessionGeneration()
    const contract = requireHTTPContract(scope, (options?.method ?? "GET").toUpperCase(), path)
    validateHTTPBody(contract, options?.body)
    const stable = withStableMutationKey(scope, path, options)
    const release = () => {
        if (stable.pendingIdentity && pendingMutationKeys.get(stable.pendingIdentity) === stable.key) pendingMutationKeys.delete(stable.pendingIdentity)
    }
    try {
        const res = await requestResponse(base, path, stable.options, stepUpRetried, generation)
        const value: T = res.status === 204 || res.headers.get("content-length") === "0" ? undefined as T : await res.json()
        assertCurrentSession(generation)
        if (contract.response && contract.response !== decode) contract.response.parse(value)
        const result = decode ? decode.parse(value) : value
        release()
        return result
    } catch (err) {
        if (!shouldRetainMutationKey(err)) release()
        throw err
    }
}

export function apiFetch<T>(path: string, options?: RequestInit, stepUpRetried = false): Promise<T> {
    return scopedJSON<T>("api", API_BASE, path, options, stepUpRetried)
}

export function apiFetchResponse(path: string, options?: RequestInit): Promise<Response> {
    const contract = requireHTTPContract("api", (options?.method ?? "GET").toUpperCase(), path)
    validateHTTPBody(contract, options?.body)
    return requestResponse(API_BASE, path, options ?? {}, false, getSessionGeneration())
}

async function scopedBlob(scope: string, base: string, path: string, options?: RequestInit, stepUpRetried = false): Promise<{ blob: Blob; filename: string }> {
    const generation = getSessionGeneration()
    const contract = requireHTTPContract(scope, (options?.method ?? "GET").toUpperCase(), path)
    validateHTTPBody(contract, options?.body)
    const res = await requestResponse(base, path, options ?? {}, stepUpRetried, generation)
    const disposition = res.headers.get("Content-Disposition") ?? ""
    const match = disposition.match(/filename="?([^";\n]+)"?/)
    const blob = await res.blob()
    assertCurrentSession(generation)
    return { blob, filename: match?.[1] ?? "export" }
}

export function apiFetchBlob(path: string, options?: RequestInit, stepUpRetried = false) {
    return scopedBlob("api", API_BASE, path, options, stepUpRetried)
}

/** A client for one backend HTTP scope other than the platform API. */
export interface ScopedClient {
    fetch<T>(path: string, options?: RequestInit, stepUpRetried?: boolean): Promise<T>
    fetchBlob(path: string, options?: RequestInit, stepUpRetried?: boolean): Promise<{ blob: Blob; filename: string }>
}

/**
 * Client for the backend HTTP scope served at /<scope>/v1 next to /api/v1, for
 * example a configuration's own surface. It shares the session, contract
 * checks and idempotency handling of apiFetch.
 */
export function createScopedClient(scope: string): ScopedClient {
    const base = API_BASE.replace(/\/api\/v1$/, `/${scope}/v1`)
    return {
        fetch: <T>(path: string, options?: RequestInit, stepUpRetried = false) => scopedJSON<T>(scope, base, path, options, stepUpRetried),
        fetchBlob: (path: string, options?: RequestInit, stepUpRetried = false) => scopedBlob(scope, base, path, options, stepUpRetried),
    }
}

/** apiFetch whose result is parsed by `schema` (an app schema over the wire contract). */
export function checkedJSON<T>(path: string, schema: z.ZodType<T>, options?: RequestInit): Promise<T> {
    return scopedJSON<T>("api", API_BASE, path, options, false, schema)
}

export function buildListQS(params?: CursorListParams): string {
    if (!params) return ""
    const entries: [string, string][] = []
    for (const [k, v] of Object.entries(params)) {
        if (v === undefined || v === null) continue
        if (k === "filter") {
            if (Array.isArray(v) && v.length > 0) {
                entries.push(["filter", JSON.stringify(v)])
            }
        } else {
            entries.push([k, String(v)])
        }
    }
    return entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
}

// ── Generic CRUD factory ────────────────────────────────────────────────

export interface CatalogApi<TRes, TCreate, TUpdate> {
    list: (params?: CursorListParams) => Promise<CursorListResponse<TRes>>
    get: (id: string) => Promise<TRes>
    create: (data: TCreate) => Promise<TRes>
    update: (id: string, data: TUpdate) => Promise<TRes>
    delete: (id: string) => Promise<void>
    setDeletionMark: (id: string, data: SetDeletionMarkRequest) => Promise<void>
    _basePath: string
}

/**
 * CRUD client of a catalog. Bodies and responses are checked by the route
 * contracts (generated from the Go DTOs), so the factory needs no schemas.
 */
export function createCatalogApi<TRes, TCreate, TUpdate>(basePath: string): CatalogApi<TRes, TCreate, TUpdate> {
    return {
        _basePath: basePath,
        list: (params?: CursorListParams) =>
            apiFetch<CursorListResponse<TRes>>(`${basePath}${buildListQS(params)}`),
        get: (id: string) =>
            apiFetch<TRes>(`${basePath}/${id}`),
        create: (data: TCreate) =>
            apiFetch<TRes>(basePath, { method: "POST", body: JSON.stringify(data) }),
        update: (id: string, data: TUpdate) =>
            apiFetch<TRes>(`${basePath}/${id}`, { method: "PUT", body: JSON.stringify(data) }),
        delete: (id: string) =>
            apiFetch<void>(`${basePath}/${id}`, { method: "DELETE" }),
        setDeletionMark: (id: string, data: SetDeletionMarkRequest) =>
            apiFetch<void>(`${basePath}/${id}/deletion-mark`, { method: "POST", body: JSON.stringify(data) }),
    }
}

// ── Generic Document CRUD factory ────────────────────────────────────────

export interface DocumentApi<TRes, TCreate, TUpdate> extends CatalogApi<TRes, TCreate, TUpdate> {
    post: (id: string) => Promise<void>
    unpost: (id: string) => Promise<void>
    updateAndRepost: (id: string, data: TUpdate) => Promise<TRes>
    getRelatedDocuments: (id: string) => Promise<import("@/types/common").RelatedDocumentsResponse>
    getMovements: (id: string) => Promise<import("@/types/common").DocumentMovementsResponse>
    batchAction: (ids: string[], action: import("@/types/common").BatchActionType) => Promise<import("@/types/common").BatchActionResponse>
    batchActionByFilter: (req: import("@/types/common").BatchActionByFilterRequest) => Promise<import("@/types/common").BatchActionResponse>
    listPrintForms: () => Promise<import("@/types/print").PrintFormSummary[]>
    _basePath: string
}

export function createDocumentApi<TRes, TCreate, TUpdate>(basePath: string): DocumentApi<TRes, TCreate, TUpdate> {
    return {
        ...createCatalogApi<TRes, TCreate, TUpdate>(basePath),
        _basePath: basePath,
        post: (id: string) =>
            apiFetch<void>(`${basePath}/${id}/post`, { method: "POST" }),
        unpost: (id: string) =>
            apiFetch<void>(`${basePath}/${id}/unpost`, { method: "POST" }),
        updateAndRepost: async (id: string, data: TUpdate) =>
            apiFetch<TRes>(`${basePath}/${id}/repost`, { method: "PUT", body: JSON.stringify(data) }),
        getRelatedDocuments: (id: string) =>
            apiFetch<import("@/types/common").RelatedDocumentsResponse>(`${basePath}/${id}/related-documents`),
        getMovements: (id: string) =>
            apiFetch<import("@/types/common").DocumentMovementsResponse>(`${basePath}/${id}/movements`),
        batchAction: (ids: string[], action: import("@/types/common").BatchActionType) =>
            apiFetch<import("@/types/common").BatchActionResponse>(`${basePath}/batch-action`, {
                method: "POST",
                body: JSON.stringify({ ids, action }),
            }),
        batchActionByFilter: (req: import("@/types/common").BatchActionByFilterRequest) =>
            apiFetch<import("@/types/common").BatchActionResponse>(`${basePath}/batch-action-by-filter`, {
                method: "POST",
                body: JSON.stringify(req),
            }),
        listPrintForms: () =>
            apiFetch<import("@/types/print").PrintFormSummary[]>(`${basePath}/print-forms`),
    }
}

/**
 * API Client – single point of contact for the backend REST API.
 *
 * All fetch calls go through `apiFetch()` which:
 *  - Prepends the base URL
 *  - Attaches auth headers + X-Tenant-ID
 *  - Handles JSON parsing
 *  - Throws typed errors
 *
 * Usage:
 *   import { api } from "@/lib/api"
 *   const items = await api.nomenclature.list()
 */

import { useAuthStore } from "@/stores/useAuthStore"
import type { TokenResponse } from "@/types/auth"
import type { CursorListResponse, CursorListParams } from "@/types/common"
import type {
    NomenclatureResponse,
    CreateNomenclatureRequest,
    UpdateNomenclatureRequest,
    SetDeletionMarkRequest,
    CounterpartyResponse,
    CreateCounterpartyRequest,
    UpdateCounterpartyRequest,
    WarehouseResponse,
    CreateWarehouseRequest,
    UpdateWarehouseRequest,
    OrganizationResponse,
    CreateOrganizationRequest,
    UpdateOrganizationRequest,
} from "@/types/catalog"
import type {
    GoodsReceiptResponse,
    CreateGoodsReceiptRequest,
    UpdateGoodsReceiptRequest,
} from "@/types/document"

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"
const TENANT_ID = process.env.NEXT_PUBLIC_TENANT_ID ?? ""

// ── Generic fetcher ─────────────────────────────────────────────────────

export interface ApiErrorBody {
    code?: string
    message?: string
    details?: Record<string, unknown>
}

export class ApiError extends Error {
    public readonly parsedBody?: ApiErrorBody

    constructor(
        public status: number,
        public statusText: string,
        public body?: unknown
    ) {
        let parsed: ApiErrorBody | undefined
        if (typeof body === "string" && body) {
            try { parsed = JSON.parse(body) } catch { /* ignore */ }
        }
        super(parsed?.message ?? `API ${status}: ${statusText}`)
        this.name = "ApiError"
        this.parsedBody = parsed
    }
}

// ── Token refresh mutex ─────────────────────────────────────────────────
// Prevents multiple concurrent refresh requests when several 401s arrive at once.
let refreshPromise: Promise<TokenResponse | null> | null = null

async function doRefreshToken(): Promise<TokenResponse | null> {
    const { tokens, setTokens, logout } = useAuthStore.getState()
    if (!tokens?.refreshToken) {
        logout()
        return null
    }

    try {
        // 1. Check if another tab recently refreshed the token
        if (typeof window !== "undefined") {
            const storedStr = localStorage.getItem("metapus-auth")
            if (storedStr) {
                try {
                    const stored = JSON.parse(storedStr)
                    const storedTokens = stored.state?.tokens
                    if (storedTokens && storedTokens.refreshToken !== tokens.refreshToken) {
                        // Another tab already refreshed the token. Absorb it.
                        setTokens(storedTokens)
                        return storedTokens
                    }
                } catch (e) {
                    // Ignore parsing errors
                }
            }
        }

        // 2. Perform the refresh request
        const res = await fetch(`${API_BASE}/auth/refresh`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                ...(TENANT_ID ? { "X-Tenant-ID": TENANT_ID } : {}),
            },
            body: JSON.stringify({ refreshToken: tokens.refreshToken }),
        })

        if (!res.ok) {
            // Only log out if it's a definitive token rejection (4xx errors)
            // Ignore 5xx server errors or network drops to avoid spurious logouts
            if (res.status >= 400 && res.status < 500) {
                logout()
            }
            return null
        }

        const newTokens: TokenResponse = await res.json()
        setTokens(newTokens)
        return newTokens
    } catch {
        // Network error / failed to fetch - preserve session, don't logout
        return null
    }
}

function refreshAccessToken(): Promise<TokenResponse | null> {
    if (!refreshPromise) {
        refreshPromise = doRefreshToken().finally(() => {
            refreshPromise = null
        })
    }
    return refreshPromise
}

// ── Paths that should never trigger a refresh retry ─────────────────────
const NO_RETRY_PATHS = ["/auth/login", "/auth/register", "/auth/refresh"]

function buildHeaders(optHeaders?: HeadersInit): Record<string, string> {
    const authHeaders: Record<string, string> = {}
    const tokens = useAuthStore.getState().tokens
    if (tokens?.accessToken) {
        authHeaders["Authorization"] = `${tokens.tokenType || "Bearer"} ${tokens.accessToken}`
    }

    const tenantHeaders: Record<string, string> = {}
    if (TENANT_ID) {
        tenantHeaders["X-Tenant-ID"] = TENANT_ID
    }

    return {
        "Content-Type": "application/json",
        ...tenantHeaders,
        ...authHeaders,
        ...(optHeaders as Record<string, string>),
    }
}

export async function apiFetch<T>(
    path: string,
    options?: RequestInit
): Promise<T> {
    const { headers: optHeaders, ...restOptions } = options ?? {}

    const res = await fetch(`${API_BASE}${path}`, {
        ...restOptions,
        headers: buildHeaders(optHeaders),
    })

    // ── 401 → attempt token refresh & retry once ────────────────────────
    if (res.status === 401 && !NO_RETRY_PATHS.includes(path)) {
        const newTokens = await refreshAccessToken()
        if (newTokens) {
            const retryRes = await fetch(`${API_BASE}${path}`, {
                ...restOptions,
                headers: buildHeaders(optHeaders),
            })

            if (!retryRes.ok) {
                const body = await retryRes.text().catch(() => undefined)
                throw new ApiError(retryRes.status, retryRes.statusText, body)
            }

            if (retryRes.status === 204 || retryRes.headers.get("content-length") === "0") {
                return undefined as T
            }
            return retryRes.json() as Promise<T>
        }
        // refresh failed → logout already called, throw original error
        const body = await res.text().catch(() => undefined)
        throw new ApiError(res.status, res.statusText, body)
    }

    if (!res.ok) {
        const body = await res.text().catch(() => undefined)
        throw new ApiError(res.status, res.statusText, body)
    }

    if (res.status === 204 || res.headers.get("content-length") === "0") {
        return undefined as T
    }

    return res.json() as Promise<T>
}

// ── Resource endpoints ──────────────────────────────────────────────────

/**
 * Build query string from ListParams.
 * - Serializes `filter` array as JSON in `?filter=` param
 * - Other params are passed as regular query params
 */
function buildListQS(params?: CursorListParams): string {
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
// Analogous to backend BaseCatalogRepo[T] — zero boilerplate per entity.

export interface CatalogApi<TRes, TCreate, TUpdate> {
    list: (params?: CursorListParams) => Promise<CursorListResponse<TRes>>
    get: (id: string) => Promise<TRes>
    create: (data: TCreate) => Promise<TRes>
    update: (id: string, data: TUpdate) => Promise<TRes>
    delete: (id: string) => Promise<void>
    setDeletionMark: (id: string, data: SetDeletionMarkRequest) => Promise<void>
}

function createCatalogApi<TRes, TCreate, TUpdate>(basePath: string): CatalogApi<TRes, TCreate, TUpdate> {
    return {
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
// Extends CatalogApi with document lifecycle methods (post, unpost, repost).
// Analogous to backend BaseDocumentHandler[T].

export interface DocumentApi<TRes, TCreate, TUpdate> extends CatalogApi<TRes, TCreate, TUpdate> {
    post: (id: string) => Promise<void>
    unpost: (id: string) => Promise<void>
    updateAndRepost: (id: string, data: TUpdate) => Promise<TRes>
}

function createDocumentApi<TRes, TCreate, TUpdate>(basePath: string): DocumentApi<TRes, TCreate, TUpdate> {
    return {
        ...createCatalogApi<TRes, TCreate, TUpdate>(basePath),
        post: (id: string) =>
            apiFetch<void>(`${basePath}/${id}/post`, { method: "POST" }),
        unpost: (id: string) =>
            apiFetch<void>(`${basePath}/${id}/unpost`, { method: "POST" }),
        updateAndRepost: (id: string, data: TUpdate) =>
            apiFetch<TRes>(`${basePath}/${id}/repost`, { method: "PUT", body: JSON.stringify(data) }),
    }
}

// Extend this object as new resources are added.
export const api = {
    auth: {
        login: (data: import("@/types/auth").LoginRequest) =>
            apiFetch<import("@/types/auth").LoginResponse>("/auth/login", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        register: (data: import("@/types/auth").RegisterRequest) =>
            apiFetch<import("@/types/auth").AuthUserResponse>("/auth/register", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        refresh: (data: import("@/types/auth").RefreshTokenRequest) =>
            apiFetch<import("@/types/auth").TokenResponse>("/auth/refresh", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        logout: () =>
            apiFetch<void>("/auth/logout", { method: "POST" }),
        me: () =>
            apiFetch<import("@/types/auth").AuthUserResponse>("/auth/me"),
        assignRole: (data: { userId: string; roleCode: string }) =>
            apiFetch<{ message: string }>("/auth/assign-role", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        revokeRole: (data: { userId: string; roleCode: string }) =>
            apiFetch<{ message: string }>("/auth/revoke-role", {
                method: "POST",
                body: JSON.stringify(data),
            }),
    },

    // ── Catalogs (1 line per entity via generic factory) ────────────────
    nomenclature: {
        ...createCatalogApi<NomenclatureResponse, CreateNomenclatureRequest, UpdateNomenclatureRequest>("/catalog/nomenclature"),
        tree: () => apiFetch<NomenclatureResponse[]>("/catalog/nomenclature/tree"),
    },

    counterparties: createCatalogApi<CounterpartyResponse, CreateCounterpartyRequest, UpdateCounterpartyRequest>("/catalog/counterparties"),

    warehouses: createCatalogApi<WarehouseResponse, CreateWarehouseRequest, UpdateWarehouseRequest>("/catalog/warehouses"),

    organizations: createCatalogApi<OrganizationResponse, CreateOrganizationRequest, UpdateOrganizationRequest>("/catalog/organizations"),

    vatRates: {
        get: (id: string) =>
            apiFetch<{ id: string; name: string; rate: string; isTaxExempt: boolean }>(`/catalog/vat-rates/${id}`),
    },

    // ── Documents (1 line per entity via generic factory) ────────────────
    goodsReceipts: createDocumentApi<GoodsReceiptResponse, CreateGoodsReceiptRequest, UpdateGoodsReceiptRequest>("/document/goods-receipt"),
    meta: {
        getFilters: (entityName: string) =>
            apiFetch<import("@/components/shared/filter-config-dialog").FilterFieldMeta[]>(
                `/meta/${entityName}/filters`
            ),
        getEntity: (entityName: string) =>
            apiFetch<{ name: string; label?: string; type: string; fields: { name: string; label?: string; type: string }[]; tableParts?: { name: string; label?: string; columns: { name: string; label?: string; type: string }[] }[] }>(
                `/meta/${entityName}`
            ),
        getMock: (entityName: string) =>
            apiFetch<Record<string, unknown>>(
                `/meta/${entityName}/mock`
            ),
    },

    preferences: {
        get: () =>
            apiFetch<import("@/types/user-prefs").UserPreferencesResponse>("/me/preferences"),

        saveInterface: (data: Partial<import("@/types/user-prefs").InterfacePrefs>) =>
            apiFetch<void>("/me/preferences/interface", {
                method: "PUT",
                body: JSON.stringify(data),
            }),

        saveListFilters: (entityType: string, data: import("@/lib/filter-utils").FilterValues) =>
            apiFetch<void>(`/me/preferences/list-filters/${entityType}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),

        saveListColumns: (entityType: string, columns: string[]) =>
            apiFetch<void>(`/me/preferences/list-columns/${entityType}`, {
                method: "PUT",
                body: JSON.stringify(columns),
            }),
    },

    settings: {
        get: () =>
            apiFetch<import("@/types/settings").SystemSettings>("/settings"),
        update: (data: Partial<import("@/types/settings").SystemSettings>) =>
            apiFetch<import("@/types/settings").SystemSettings>("/settings", {
                method: "PUT",
                body: JSON.stringify(data),
            }),
    },

    users: {
        list: (search?: string) =>
            apiFetch<{ items: import("@/types/security").UserResponse[]; total: number }>(
                `/auth/users${search ? `?search=${encodeURIComponent(search)}` : ""}`
            ),
        get: (id: string) =>
            apiFetch<import("@/types/security").UserResponse>(`/auth/users/${id}`),
        create: (data: import("@/types/security").CreateUserAdminRequest) =>
            apiFetch<import("@/types/security").UserResponse>("/auth/users", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: import("@/types/security").UpdateUserRequest) =>
            apiFetch<import("@/types/security").UserResponse>(`/auth/users/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        effectiveAccess: (id: string) =>
            apiFetch<import("@/types/security").EffectiveAccessResponse>(`/auth/users/${id}/effective-access`),
        impersonate: (id: string) =>
            apiFetch<{ tokens: { accessToken: string; refreshToken: string; expiresAt: string; tokenType: string }; user: import("@/types/security").UserResponse }>(`/auth/users/${id}/impersonate`, {
                method: "POST",
            }),
    },

    roles: {
        list: () =>
            apiFetch<{ items: import("@/types/security").RoleResponse[] }>("/auth/roles"),
        getPermissions: (roleId: string) =>
            apiFetch<{ items: import("@/types/security").PermissionResponse[] }>(`/auth/roles/${roleId}/permissions`),
    },

    permissions: {
        list: () =>
            apiFetch<{ items: import("@/types/security").PermissionResponse[] }>("/auth/permissions"),
    },

    security: {
        profiles: {
            list: () =>
                apiFetch<{ items: import("@/types/security").SecurityProfileResponse[] }>("/security/profiles"),
            get: (id: string) =>
                apiFetch<import("@/types/security").SecurityProfileResponse>(`/security/profiles/${id}`),
            create: (data: import("@/types/security").CreateSecurityProfileRequest) =>
                apiFetch<import("@/types/security").SecurityProfileResponse>("/security/profiles", {
                    method: "POST",
                    body: JSON.stringify(data),
                }),
            update: (id: string, data: import("@/types/security").UpdateSecurityProfileRequest) =>
                apiFetch<import("@/types/security").SecurityProfileResponse>(`/security/profiles/${id}`, {
                    method: "PUT",
                    body: JSON.stringify(data),
                }),
            delete: (id: string) =>
                apiFetch<void>(`/security/profiles/${id}`, { method: "DELETE" }),
            listUsers: (profileId: string) =>
                apiFetch<{ items: import("@/types/security").ProfileUserItem[] }>(`/security/profiles/${profileId}/users`),
            assignUser: (profileId: string, userId: string) =>
                apiFetch<void>(`/security/profiles/${profileId}/users`, {
                    method: "POST",
                    body: JSON.stringify({ userId }),
                }),
            removeUser: (profileId: string, userId: string) =>
                apiFetch<void>(`/security/profiles/${profileId}/users/${userId}`, {
                    method: "DELETE",
                }),
            auditHistory: (profileId: string, limit?: number) =>
                apiFetch<{ items: import("@/types/security").AuditEntryResponse[] }>(
                    `/security/profiles/${profileId}/audit${limit ? `?limit=${limit}` : ""}`
                ),
        },
        rules: {
            list: (profileId: string) =>
                apiFetch<import("@/types/security").PolicyRuleResponse[]>(`/security/profiles/${profileId}/rules`),
            create: (profileId: string, data: import("@/types/security").CreatePolicyRuleRequest) =>
                apiFetch<import("@/types/security").PolicyRuleResponse>(`/security/profiles/${profileId}/rules`, {
                    method: "POST",
                    body: JSON.stringify(data),
                }),
            update: (profileId: string, ruleId: string, data: import("@/types/security").UpdatePolicyRuleRequest) =>
                apiFetch<import("@/types/security").PolicyRuleResponse>(`/security/profiles/${profileId}/rules/${ruleId}`, {
                    method: "PUT",
                    body: JSON.stringify(data),
                }),
            delete: (profileId: string, ruleId: string) =>
                apiFetch<void>(`/security/profiles/${profileId}/rules/${ruleId}`, {
                    method: "DELETE",
                }),
            validate: (expression: string) =>
                apiFetch<import("@/types/security").ValidateExpressionResponse>("/security/rules/validate", {
                    method: "POST",
                    body: JSON.stringify({ expression }),
                }),
            test: (expression: string, doc?: Record<string, unknown>, action?: string) =>
                apiFetch<import("@/types/security").TestExpressionResponse>("/security/rules/test", {
                    method: "POST",
                    body: JSON.stringify({ expression, doc, action }),
                }),
        },
    },
} as const

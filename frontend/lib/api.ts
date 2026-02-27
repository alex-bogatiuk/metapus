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
import type { ListResponse, ListParams } from "@/types/common"
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
    SetDocumentDeletionMarkRequest,
} from "@/types/document"

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "/api/v1"
const TENANT_ID = process.env.NEXT_PUBLIC_TENANT_ID ?? ""

// ── Generic fetcher ─────────────────────────────────────────────────────

export class ApiError extends Error {
    constructor(
        public status: number,
        public statusText: string,
        public body?: unknown
    ) {
        super(`API ${status}: ${statusText}`)
        this.name = "ApiError"
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
        const res = await fetch(`${API_BASE}/auth/refresh`, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                ...(TENANT_ID ? { "X-Tenant-ID": TENANT_ID } : {}),
            },
            body: JSON.stringify({ refreshToken: tokens.refreshToken }),
        })

        if (!res.ok) {
            logout()
            return null
        }

        const newTokens: TokenResponse = await res.json()
        setTokens(newTokens)
        return newTokens
    } catch {
        logout()
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

async function apiFetch<T>(
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
    },

    nomenclature: {
        list: (params?: ListParams) => {
            const qs = params ? "?" + new URLSearchParams(
                Object.entries(params)
                    .filter(([, v]) => v !== undefined && v !== null)
                    .map(([k, v]) => [k, String(v)])
            ).toString() : ""
            return apiFetch<ListResponse<NomenclatureResponse>>(`/catalog/nomenclature${qs}`)
        },
        get: (id: string) =>
            apiFetch<NomenclatureResponse>(`/catalog/nomenclature/${id}`),
        create: (data: CreateNomenclatureRequest) =>
            apiFetch<NomenclatureResponse>("/catalog/nomenclature", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: UpdateNomenclatureRequest) =>
            apiFetch<NomenclatureResponse>(`/catalog/nomenclature/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/catalog/nomenclature/${id}`, { method: "DELETE" }),
        setDeletionMark: (id: string, data: SetDeletionMarkRequest) =>
            apiFetch<void>(`/catalog/nomenclature/${id}/deletion-mark`, {
                method: "POST",
                body: JSON.stringify(data),
            }),
        tree: () =>
            apiFetch<NomenclatureResponse[]>("/catalog/nomenclature/tree"),
    },

    counterparties: {
        list: (params?: ListParams) => {
            const qs = params ? "?" + new URLSearchParams(
                Object.entries(params)
                    .filter(([, v]) => v !== undefined && v !== null)
                    .map(([k, v]) => [k, String(v)])
            ).toString() : ""
            return apiFetch<ListResponse<CounterpartyResponse>>(`/catalog/counterparties${qs}`)
        },
        get: (id: string) =>
            apiFetch<CounterpartyResponse>(`/catalog/counterparties/${id}`),
        create: (data: CreateCounterpartyRequest) =>
            apiFetch<CounterpartyResponse>("/catalog/counterparties", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: UpdateCounterpartyRequest) =>
            apiFetch<CounterpartyResponse>(`/catalog/counterparties/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/catalog/counterparties/${id}`, { method: "DELETE" }),
        setDeletionMark: (id: string, data: SetDeletionMarkRequest) =>
            apiFetch<void>(`/catalog/counterparties/${id}/deletion-mark`, {
                method: "POST",
                body: JSON.stringify(data),
            }),
    },

    warehouses: {
        list: (params?: ListParams) => {
            const qs = params ? "?" + new URLSearchParams(
                Object.entries(params)
                    .filter(([, v]) => v !== undefined && v !== null)
                    .map(([k, v]) => [k, String(v)])
            ).toString() : ""
            return apiFetch<ListResponse<WarehouseResponse>>(`/catalog/warehouses${qs}`)
        },
        get: (id: string) =>
            apiFetch<WarehouseResponse>(`/catalog/warehouses/${id}`),
        create: (data: CreateWarehouseRequest) =>
            apiFetch<WarehouseResponse>("/catalog/warehouses", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: UpdateWarehouseRequest) =>
            apiFetch<WarehouseResponse>(`/catalog/warehouses/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/catalog/warehouses/${id}`, { method: "DELETE" }),
        setDeletionMark: (id: string, data: SetDeletionMarkRequest) =>
            apiFetch<void>(`/catalog/warehouses/${id}/deletion-mark`, {
                method: "POST",
                body: JSON.stringify(data),
            }),
    },

    organizations: {
        list: (params?: ListParams) => {
            const qs = params ? "?" + new URLSearchParams(
                Object.entries(params)
                    .filter(([, v]) => v !== undefined && v !== null)
                    .map(([k, v]) => [k, String(v)])
            ).toString() : ""
            return apiFetch<ListResponse<OrganizationResponse>>(`/catalog/organizations${qs}`)
        },
        get: (id: string) =>
            apiFetch<OrganizationResponse>(`/catalog/organizations/${id}`),
        create: (data: CreateOrganizationRequest) =>
            apiFetch<OrganizationResponse>("/catalog/organizations", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: UpdateOrganizationRequest) =>
            apiFetch<OrganizationResponse>(`/catalog/organizations/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/catalog/organizations/${id}`, { method: "DELETE" }),
        setDeletionMark: (id: string, data: SetDeletionMarkRequest) =>
            apiFetch<void>(`/catalog/organizations/${id}/deletion-mark`, {
                method: "POST",
                body: JSON.stringify(data),
            }),
    },

    goodsReceipts: {
        list: (params?: ListParams) => {
            const qs = params ? "?" + new URLSearchParams(
                Object.entries(params)
                    .filter(([, v]) => v !== undefined && v !== null)
                    .map(([k, v]) => [k, String(v)])
            ).toString() : ""
            return apiFetch<ListResponse<GoodsReceiptResponse>>(`/document/goods-receipt${qs}`)
        },
        get: (id: string) =>
            apiFetch<GoodsReceiptResponse>(`/document/goods-receipt/${id}`),
        create: (data: CreateGoodsReceiptRequest) =>
            apiFetch<GoodsReceiptResponse>("/document/goods-receipt", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: UpdateGoodsReceiptRequest) =>
            apiFetch<GoodsReceiptResponse>(`/document/goods-receipt/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/document/goods-receipt/${id}`, { method: "DELETE" }),
        post: (id: string) =>
            apiFetch<void>(`/document/goods-receipt/${id}/post`, {
                method: "POST",
            }),
        unpost: (id: string) =>
            apiFetch<void>(`/document/goods-receipt/${id}/unpost`, {
                method: "POST",
            }),
        setDeletionMark: (id: string, data: SetDocumentDeletionMarkRequest) =>
            apiFetch<void>(`/document/goods-receipt/${id}/deletion-mark`, {
                method: "POST",
                body: JSON.stringify(data),
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
        list: () =>
            apiFetch<import("@/types/settings").UserRecord[]>("/users"),
        get: (id: string) =>
            apiFetch<import("@/types/settings").UserRecord>(`/users/${id}`),
        create: (data: unknown) =>
            apiFetch<import("@/types/settings").UserRecord>("/users", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: unknown) =>
            apiFetch<import("@/types/settings").UserRecord>(`/users/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/users/${id}`, { method: "DELETE" }),
    },

    roles: {
        list: () =>
            apiFetch<import("@/types/settings").RoleRecord[]>("/roles"),
        get: (id: string) =>
            apiFetch<import("@/types/settings").RoleRecord>(`/roles/${id}`),
        create: (data: unknown) =>
            apiFetch<import("@/types/settings").RoleRecord>("/roles", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: unknown) =>
            apiFetch<import("@/types/settings").RoleRecord>(`/roles/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/roles/${id}`, { method: "DELETE" }),
    },
} as const

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
    UnitResponse,
    CreateUnitRequest,
    UpdateUnitRequest,
    CurrencyResponse,
    CreateCurrencyRequest,
    UpdateCurrencyRequest,
    ContractResponse,
    CreateContractRequest,
    UpdateContractRequest,
    VATRateResponse,
    CreateVATRateRequest,
    UpdateVATRateRequest,
} from "@/types/catalog"
import type {
    GoodsReceiptResponse,
    CreateGoodsReceiptRequest,
    UpdateGoodsReceiptRequest,
    GoodsIssueResponse,
    CreateGoodsIssueRequest,
    UpdateGoodsIssueRequest,
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
 * Build query string from CursorListParams.
 * - Serializes `filter` array as JSON in `?filter=` param
 * - Other params are passed as regular query params
 */
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
    getRelatedDocuments: (id: string) => Promise<import("@/types/common").RelatedDocumentsResponse>
    getMovements: (id: string) => Promise<import("@/types/common").DocumentMovementsResponse>
    /** Batch action: post/unpost/setDeletionMark/clearDeletionMark for multiple documents in one HTTP call. */
    batchAction: (ids: string[], action: import("@/types/common").BatchActionType) => Promise<import("@/types/common").BatchActionResponse>
    /** Filter-based batch action: server resolves matching IDs from filter (virtual "select all"). */
    batchActionByFilter: (req: import("@/types/common").BatchActionByFilterRequest) => Promise<import("@/types/common").BatchActionResponse>
    /** Base API path (used by SSE streaming). */
    _basePath: string
}

function createDocumentApi<TRes, TCreate, TUpdate>(basePath: string): DocumentApi<TRes, TCreate, TUpdate> {
    return {
        ...createCatalogApi<TRes, TCreate, TUpdate>(basePath),
        _basePath: basePath,
        post: (id: string) =>
            apiFetch<void>(`${basePath}/${id}/post`, { method: "POST" }),
        unpost: (id: string) =>
            apiFetch<void>(`${basePath}/${id}/unpost`, { method: "POST" }),
        updateAndRepost: (id: string, data: TUpdate) =>
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
    }
}

// ── Admin Tenant Types (Cloud Control Plane) ────────────────────────────

export interface TenantSummary {
    id: string
    slug: string
    displayName: string
    dbName: string
    status: string
    plan: string
    schemaVersion: number
    versionGroup: string
    createdAt: string
    updatedAt: string
    schemaUpToDate: boolean
}

export interface TenantListResponse {
    items: TenantSummary[]
    total: number
    activeCount: number
    outdatedCount: number
    versionGroups: Record<string, number>
    expectedSchema: number
    serverVersion: string
}

export interface MigrationStatusResponse {
    tenantId: string
    status: string
    preUpdateVersions: Record<string, number> | null
    lastError: string | null
    updatedAt: string | null
}

export interface TenantStats {
    totalTenants: number
    activeTenants: number
    suspendedTenants: number
    outdatedSchemas: number
    expectedSchemaVersion: number
    versionGroups: Record<string, number>
    schemaVersions: Record<string, number>
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

    units: createCatalogApi<UnitResponse, CreateUnitRequest, UpdateUnitRequest>("/catalog/units"),
    
    currencies: createCatalogApi<CurrencyResponse, CreateCurrencyRequest, UpdateCurrencyRequest>("/catalog/currencies"),

    contracts: createCatalogApi<ContractResponse, CreateContractRequest, UpdateContractRequest>("/catalog/contracts"),

    vatRates: createCatalogApi<VATRateResponse, CreateVATRateRequest, UpdateVATRateRequest>("/catalog/vat-rates"),

    // ── Documents (1 line per entity via generic factory) ────────────────
    goodsReceipts: createDocumentApi<GoodsReceiptResponse, CreateGoodsReceiptRequest, UpdateGoodsReceiptRequest>("/document/goods-receipt"),
    goodsIssues: createDocumentApi<GoodsIssueResponse, CreateGoodsIssueRequest, UpdateGoodsIssueRequest>("/document/goods-issue"),

    // ── Registers (stock) ───────────────────────────────────────────────
    stock: {
        /** Get stock balances for a warehouse. Returns { items: [{ productId, quantity, ... }] }. */
        getBalancesByWarehouse: (warehouseId: string) =>
            apiFetch<{ items: { warehouseId: string; productId: string; quantity: number; lastMovementAt?: string }[] }>(
                `/registers/stock/balances?warehouseId=${encodeURIComponent(warehouseId)}`
            ),
    },

    // ── Automations ──────────────────────────────────────────────────────
    automation: {
        // Accounts
        accounts: {
            list: () =>
                apiFetch<import("@/types/automation").AutomationAccount[]>("/system/automation-accounts"),
            get: (id: string) =>
                apiFetch<import("@/types/automation").AutomationAccount>(`/system/automation-accounts/${id}`),
            create: (data: import("@/types/automation").CreateAccountRequest) =>
                apiFetch<{ id: string }>("/system/automation-accounts", {
                    method: "POST",
                    body: JSON.stringify(data),
                }),
            update: (id: string, data: import("@/types/automation").UpdateAccountRequest) =>
                apiFetch<import("@/types/automation").AutomationAccount>(`/system/automation-accounts/${id}`, {
                    method: "PUT",
                    body: JSON.stringify(data),
                }),
            delete: (id: string) =>
                apiFetch<void>(`/system/automation-accounts/${id}`, { method: "DELETE" }),
            updateCredentials: (id: string, credentials: string) =>
                apiFetch<void>(`/system/automation-accounts/${id}/credentials`, {
                    method: "PUT",
                    body: JSON.stringify({ credentials }),
                }),
            test: (id: string) =>
                apiFetch<{ message?: string }>(`/system/automation-accounts/${id}/test`, { method: "POST" }),
        },
        // Channels
        channels: {
            list: (accountId?: string) => {
                const qs = accountId ? `?accountId=${encodeURIComponent(accountId)}` : ""
                return apiFetch<import("@/types/automation").AutomationChannel[]>(`/system/automation-channels${qs}`)
            },
            get: (id: string) =>
                apiFetch<import("@/types/automation").AutomationChannel>(`/system/automation-channels/${id}`),
            create: (data: import("@/types/automation").CreateChannelRequest) =>
                apiFetch<{ id: string }>("/system/automation-channels", {
                    method: "POST",
                    body: JSON.stringify(data),
                }),
            update: (id: string, data: import("@/types/automation").UpdateChannelRequest) =>
                apiFetch<import("@/types/automation").AutomationChannel>(`/system/automation-channels/${id}`, {
                    method: "PUT",
                    body: JSON.stringify(data),
                }),
            delete: (id: string) =>
                apiFetch<void>(`/system/automation-channels/${id}`, { method: "DELETE" }),
        },
        // Rules
        rules: {
            list: (eventType?: string) => {
                const qs = eventType ? `?eventType=${encodeURIComponent(eventType)}` : ""
                return apiFetch<import("@/types/automation").AutomationRule[]>(`/system/automation-rules${qs}`)
            },
            get: (id: string) =>
                apiFetch<import("@/types/automation").AutomationRule>(`/system/automation-rules/${id}`),
            create: (data: import("@/types/automation").CreateRuleRequest) =>
                apiFetch<{ id: string }>("/system/automation-rules", {
                    method: "POST",
                    body: JSON.stringify(data),
                }),
            update: (id: string, data: import("@/types/automation").UpdateRuleRequest) =>
                apiFetch<import("@/types/automation").AutomationRule>(`/system/automation-rules/${id}`, {
                    method: "PUT",
                    body: JSON.stringify(data),
                }),
            delete: (id: string) =>
                apiFetch<void>(`/system/automation-rules/${id}`, { method: "DELETE" }),
            toggle: (id: string) =>
                apiFetch<{ isActive: boolean }>(`/system/automation-rules/${id}/toggle`, { method: "POST" }),
            test: (data: import("@/types/automation").TestRuleRequest) =>
                apiFetch<import("@/types/automation").TestRuleResponse>("/system/automation-rules/test", {
                    method: "POST",
                    body: JSON.stringify(data),
                }),
        },
        // History
        history: {
            list: (params?: { limit?: number; offset?: number; ruleId?: string; status?: string; channelId?: string; from?: string; to?: string }) => {
                const entries: [string, string][] = []
                if (params?.limit) entries.push(["limit", String(params.limit)])
                if (params?.offset) entries.push(["offset", String(params.offset)])
                if (params?.ruleId) entries.push(["ruleId", params.ruleId])
                if (params?.status) entries.push(["status", params.status])
                if (params?.channelId) entries.push(["channelId", params.channelId])
                if (params?.from) entries.push(["from", params.from])
                if (params?.to) entries.push(["to", params.to])
                const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
                return apiFetch<import("@/types/automation").HistoryListResponse>(`/system/automation-history${qs}`)
            },
            get: (id: string) =>
                apiFetch<import("@/types/automation").AutomationHistoryEntry>(`/system/automation-history/${id}`),
            replay: (id: string) =>
                apiFetch<{ id: string }>(`/system/automation-history/${id}/replay`, { method: "POST" }),
        },
        // Meta (enum values + event types)
        meta: {
            get: () =>
                apiFetch<import("@/types/automation").AutomationMeta>("/system/automation-meta"),
            eventTypes: () =>
                apiFetch<import("@/types/automation").EventTypeGroup[]>("/system/automation/meta/event-types"),
            entityFields: (entityType: string) =>
                apiFetch<{ name: string; label: string; type: string }[]>(`/system/automation/meta/entity-fields/${encodeURIComponent(entityType)}`),
        },
    },

    meta: {
        listEntities: () =>
            apiFetch<import("@/types/metadata").EntityMeta[]>("/meta/entities"),
        getFilters: (entityName: string) =>
            apiFetch<import("@/components/shared/filter-config-dialog").FilterFieldMeta[]>(
                `/meta/${entityName}/filters`
            ),
        getEntity: (entityName: string) =>
            apiFetch<{ name: string; type: string; fields: { name: string; label?: string; type: string }[]; tableParts?: { name: string; label?: string; columns: { name: string; label?: string; type: string }[] }[] }>(
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

        saveListColumns: (entityType: string, data: unknown) =>
            apiFetch<void>(`/me/preferences/list-columns/${entityType}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),

        saveDashboardLayout: (layout: import("@/types/dashboard").DashboardLayout) =>
            apiFetch<void>("/me/preferences/dashboard-layout", {
                method: "PUT",
                body: JSON.stringify(layout),
            }),
    },

    reports: {
        getStockBalance: (params?: { warehouseId?: string[]; productId?: string[]; excludeZero?: boolean }) => {
            const entries: [string, string][] = []
            if (params?.warehouseId) params.warehouseId.forEach((id) => entries.push(["warehouseId", id]))
            if (params?.productId) params.productId.forEach((id) => entries.push(["productId", id]))
            if (params?.excludeZero !== undefined) entries.push(["excludeZero", String(params.excludeZero)])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return apiFetch<import("@/types/reports").StockBalanceReportResponse>(`/reports/stock-balance${qs}`)
        },

        getStockTurnover: (params: { fromDate: string; toDate: string; warehouseId?: string[]; productId?: string[] }) => {
            const entries: [string, string][] = [["fromDate", params.fromDate], ["toDate", params.toDate]]
            if (params.warehouseId) params.warehouseId.forEach((id) => entries.push(["warehouseId", id]))
            if (params.productId) params.productId.forEach((id) => entries.push(["productId", id]))
            const qs = "?" + new URLSearchParams(entries).toString()
            return apiFetch<import("@/types/reports").StockTurnoverReportResponse>(`/reports/stock-turnover${qs}`)
        },

        getDocumentJournal: (params?: { fromDate?: string; toDate?: string; documentType?: string[]; posted?: boolean; limit?: number }) => {
            const entries: [string, string][] = []
            if (params?.fromDate) entries.push(["fromDate", params.fromDate])
            if (params?.toDate) entries.push(["toDate", params.toDate])
            if (params?.documentType) params.documentType.forEach((t) => entries.push(["documentType", t]))
            if (params?.posted !== undefined) entries.push(["posted", String(params.posted)])
            if (params?.limit) entries.push(["limit", String(params.limit)])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return apiFetch<import("@/types/reports").DocumentJournalResponse>(`/reports/document-journal${qs}`)
        },
    },

    settings: {
        get: () =>
            apiFetch<import("@/types/settings").SystemSettings>("/settings"),
        updateSection: (section: string, data: unknown, version: number) =>
            apiFetch<import("@/types/settings").SystemSettings>(`/settings/${section}`, {
                method: "PATCH",
                body: JSON.stringify({ data, version }),
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
        get: (id: string) =>
            apiFetch<import("@/types/security").RoleResponse>(`/auth/roles/${id}`),
        create: (data: import("@/types/security").CreateRoleRequest) =>
            apiFetch<import("@/types/security").RoleResponse>("/auth/roles", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: import("@/types/security").UpdateRoleRequest) =>
            apiFetch<import("@/types/security").RoleResponse>(`/auth/roles/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }),
        delete: (id: string) =>
            apiFetch<{ message: string; affectedUsers: number }>(`/auth/roles/${id}`, {
                method: "DELETE",
            }),
        getPermissions: (roleId: string) =>
            apiFetch<{ items: import("@/types/security").PermissionResponse[] }>(`/auth/roles/${roleId}/permissions`),
        setPermissions: (roleId: string, permissionIds: string[]) =>
            apiFetch<{ message: string }>(`/auth/roles/${roleId}/permissions`, {
                method: "PUT",
                body: JSON.stringify({ permissionIds }),
            }),
    },

    permissions: {
        list: () =>
            apiFetch<{ items: import("@/types/security").PermissionResponse[] }>("/auth/permissions"),
    },

    system: {
        /** Public endpoint — no auth/tenant required. */
        version: async (): Promise<{ version: string; buildTime: string; expectedSchemaVersion: number }> => {
            const res = await fetch(`${API_BASE}/system/version`, {
                headers: TENANT_ID ? { "X-Tenant-ID": TENANT_ID } : {},
            })
            if (!res.ok) throw new ApiError(res.status, res.statusText)
            return res.json()
        },

        eventLog: {
            list: (params?: Record<string, string | undefined>) => {
                const entries = Object.entries(params ?? {}).filter(([, v]) => v !== undefined && v !== "") as [string, string][]
                const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
                return apiFetch<import("@/types/common").CursorListResponse<import("@/types/event-log").EventLogEntry>>(
                    `/system/event-log${qs}`
                )
            },
            get: (id: string) =>
                apiFetch<import("@/types/event-log").EventLogEntry>(`/system/event-log/${id}`),
            trace: (traceId: string) =>
                apiFetch<{ items: import("@/types/event-log").EventLogEntry[] }>(
                    `/system/event-log/trace/${traceId}`
                ),
            stats: (params: { dateFrom?: string; dateTo?: string } = {}) => {
                const entries = Object.entries(params).filter(([, v]) => v !== undefined) as [string, string][]
                const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
                return apiFetch<import("@/types/event-log").EventLogStats>(
                    `/system/event-log/stats${qs}`
                )
            },
        },

        findReferences: (data: { entityName: string; entityId: string }) =>
            apiFetch<{ items: import("@/types/common").FoundReference[]; total: number }>("/system/find-references", {
                method: "POST",
                body: JSON.stringify(data),
            }),

        markedObjects: {
            list: () =>
                apiFetch<{ items: import("@/types/common").MarkedObject[]; total: number }>("/system/marked-objects"),
            delete: (items: { entityName: string; entityId: string }[]) =>
                apiFetch<{ deleted: number; skipped: number; errors: number }>("/system/marked-objects/delete", {
                    method: "POST",
                    body: JSON.stringify({ items }),
                }),
        },
        notifications: {
            list: (params?: { limit?: number; unreadOnly?: boolean }) => {
                const qs = new URLSearchParams()
                if (params?.limit) qs.set("limit", params.limit.toString())
                if (params?.unreadOnly) qs.set("unreadOnly", "true")
                return apiFetch<import("@/types/notification").NotificationListResponse>(`/system/notifications?${qs.toString()}`)
            },
            markAsRead: (id: string) =>
                apiFetch<void>(`/system/notifications/${id}/read`, { method: "PUT" }),
            markAllAsRead: () =>
                apiFetch<void>(`/system/notifications/mark-all-read`, { method: "PUT" }),
        },
    },

    admin: {
        tenants: {
            list: () =>
                apiFetch<TenantListResponse>("/admin/tenants"),
            get: (id: string) =>
                apiFetch<TenantSummary>(`/admin/tenants/${id}`),
            stats: () =>
                apiFetch<TenantStats>("/admin/tenants/stats"),
            promote: (id: string, versionGroup: string) =>
                apiFetch<{ message: string; tenantId: string; slug: string; oldGroup: string; newGroup: string }>(
                    `/admin/tenants/${id}/version-group`,
                    { method: "PUT", body: JSON.stringify({ versionGroup }) }
                ),
            updateSchemaVersion: (id: string, schemaVersion: number) =>
                apiFetch<{ message: string; tenantId: string; schemaVersion: number; upToDate: boolean }>(
                    `/admin/tenants/${id}/schema-version`,
                    { method: "PUT", body: JSON.stringify({ schemaVersion }) }
                ),
            triggerUpdate: (id: string) =>
                apiFetch<{ message: string; tenantId: string; status: string }>(
                    `/admin/tenants/${id}/update`,
                    { method: "POST" }
                ),
            retryUpdate: (id: string) =>
                apiFetch<{ message: string; tenantId: string; status: string }>(
                    `/admin/tenants/${id}/retry-update`,
                    { method: "POST" }
                ),
            rollbackUpdate: (id: string) =>
                apiFetch<{ message: string; tenantId: string; status: string }>(
                    `/admin/tenants/${id}/rollback-update`,
                    { method: "POST" }
                ),
            migrationStatus: (id: string) =>
                apiFetch<MigrationStatusResponse>(`/admin/tenants/${id}/migration-status`),
        },
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

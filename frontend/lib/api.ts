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
 *   const items = await api.counterparties.list()
 */

// ── Core exports ────────────────────────────────────────────────────────
export {
    API_BASE,
    TENANT_ID,
    UUID_PATH_PARAM_RE,
    assertUuidPathParam,
    ApiError,
    type ApiErrorBody,
    bootstrapAccessToken,
    refreshAccessToken,
    idempotencyHeaders,
    shouldRetainMutationKey,
    apiFetch,
    apiFetchResponse,
    apiFetchBlob,
    createScopedClient,
    type ScopedClient,
    checkedJSON,
    buildListQS,
    type CatalogApi,
    createCatalogApi,
    type DocumentApi,
    createDocumentApi,
} from "./api/core"

// ── Metadata & Caching exports ──────────────────────────────────────────
export {
    type TenantSummary,
    type TenantListResponse,
    type MetadataEntityResponse,
    getCachedMetaEntity,
    getCachedMetaFilters,
    readCachedMetaFilters,
    readCachedMetaEntity,
    getCachedListViews,
    readCachedListViews,
} from "./api/meta"

// ── Domain sub-APIs ─────────────────────────────────────────────────────
import { authApi, usersApi, rolesApi, permissionsApi } from "./api/auth"
import { catalogsApi } from "./api/catalogs"
import { automationApi } from "./api/automation"
import { metaApi, preferencesApi, listViewsApi, reportsApi, settingsApi } from "./api/meta"
import { securityApi } from "./api/security"
import { searchApi, systemApi, adminApi } from "./api/system"

export const api = {
    auth: authApi,
    users: usersApi,
    roles: rolesApi,
    permissions: permissionsApi,

    // Catalogs
    ...catalogsApi,

    // Automation
    automation: automationApi,

    // Meta, Preferences, List Views, Reports, Settings
    meta: metaApi,
    preferences: preferencesApi,
    listViews: listViewsApi,
    reports: reportsApi,
    settings: settingsApi,

    // Security
    security: securityApi,

    // System & Admin
    search: searchApi,
    system: systemApi,
    admin: adminApi,
} as const

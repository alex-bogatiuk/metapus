import { z } from "zod"
import * as wireSchemas from "@/lib/generated/http-contract-schemas"
import { jsonObjectSchema, type JsonObject, type JsonValue } from "@/lib/json-value"
import { assertCurrentSession, getSessionGeneration, onScopeReset } from "@/lib/auth-scope"
import { entitySummarySchema, entityMetadataSchema, filterFieldMetadataSchema, type EntityMetadata } from "@/lib/schemas/metadata"
import { preferencesSchema } from "@/lib/schemas/preferences"
import { listViewSchema, createListViewSchema, updateListViewSchema } from "@/lib/schemas/list-views"
import { reportVariantSchema, variantConfigSchema } from "@/lib/schemas/report-variants"
import { systemSettingsSchema, type SettingsSection } from "@/lib/schemas/settings"
import { type RawUnits, rawUnitsSchema } from "@/lib/money"
import type { ListView } from "@/types/list-view"
import type { FilterFieldMeta } from "@/types/filter"
import type { DocumentJournalItem, DocumentJournalResponse } from "@/types/reports"
import { apiFetch, checkedJSON } from "./core"

// ── Admin tenant monitoring types ──────────────────────────────────────

export interface TenantSummary {
    id: string
    slug: string
    displayName: string
    dbName: string
    status: string
    plan: string
    schemaVersion: number
    createdAt: string
    updatedAt: string
    schemaUpToDate: boolean
}

export interface TenantListResponse {
    items: TenantSummary[]
    total: number
    activeCount: number
    outdatedCount: number
    expectedSchema: number
}

export type MetadataEntityResponse = EntityMetadata

interface DatasetReportResponse<TItem> {
    items: TItem[]
    totalItems: number
}

interface DocumentJournalDatasetRow {
    id: string
    document_type: string
    number: string
    date: string
    posted: boolean
    counterparty_name?: string | null
    total_amount?: RawUnits | null
    currency?: string | null
    currency_id?: string | null
    currency_decimal_places?: number | null
    description?: string | null
}

const documentJournalResultSchema = z.object({
    items: z.array(z.object({
        id: z.string(), document_type: z.string(), number: z.string(), date: z.string(), posted: z.boolean(),
        counterparty_name: z.string().nullable().optional(), total_amount: rawUnitsSchema.nullable(),
        currency: z.string().min(1), currency_id: z.string().min(1), currency_decimal_places: z.int().min(0).max(18),
        description: z.string().nullable().optional(),
    })),
    totalItems: z.int().nonnegative(),
}) satisfies z.ZodType<DatasetReportResponse<DocumentJournalDatasetRow>>

// ── Caches ─────────────────────────────────────────────────────────────

const metaEntityCache = new Map<string, Promise<MetadataEntityResponse>>()
const metaEntityValueCache = new Map<string, MetadataEntityResponse>()
const metaFiltersCache = new Map<string, Promise<FilterFieldMeta[]>>()
const metaFiltersValueCache = new Map<string, FilterFieldMeta[]>()
const listViewsCache = new Map<string, Promise<ListView[]>>()
const listViewsValueCache = new Map<string, ListView[]>()

onScopeReset(() => {
    metaEntityCache.clear(); metaEntityValueCache.clear()
    metaFiltersCache.clear(); metaFiltersValueCache.clear()
    listViewsCache.clear(); listViewsValueCache.clear()
})

function normalizeMetadataName(entityName: string): string {
    const normalized = entityName.trim()
    if (!normalized) {
        throw new Error("Metadata entity name is required")
    }
    return normalized
}

function normalizeListViewEntityType(entityType: string): string {
    const normalized = entityType.trim()
    if (!normalized) {
        throw new Error("List view entity type is required")
    }
    return normalized
}

export function getCachedMetaEntity(entityName: string): Promise<MetadataEntityResponse> {
    const generation = getSessionGeneration()
    const normalized = normalizeMetadataName(entityName)
    const value = metaEntityValueCache.get(normalized)
    if (value) return Promise.resolve(value)

    const cached = metaEntityCache.get(normalized)
    if (cached) return cached

    const request = apiFetch<JsonValue>(
        `/meta/${encodeURIComponent(normalized)}`
    ).then(val => entityMetadataSchema.parse(val)).then((data) => {
        assertCurrentSession(generation)
        metaEntityValueCache.set(normalized, data)
        return data
    }).catch((err) => {
        assertCurrentSession(generation)
        metaEntityCache.delete(normalized)
        throw err
    })

    metaEntityCache.set(normalized, request)
    return request
}

export function getCachedMetaFilters(entityName: string): Promise<FilterFieldMeta[]> {
    const generation = getSessionGeneration()
    const normalized = normalizeMetadataName(entityName)
    const value = metaFiltersValueCache.get(normalized)
    if (value) return Promise.resolve(value)

    const cached = metaFiltersCache.get(normalized)
    if (cached) return cached

    const request = apiFetch<JsonValue>(
        `/meta/${encodeURIComponent(normalized)}/filters`
    ).then(val => z.array(filterFieldMetadataSchema).parse(val)).then((data) => {
        assertCurrentSession(generation)
        metaFiltersValueCache.set(normalized, data)
        return data
    }).catch((err) => {
        assertCurrentSession(generation)
        metaFiltersCache.delete(normalized)
        throw err
    })

    metaFiltersCache.set(normalized, request)
    return request
}

export function readCachedMetaFilters(entityName: string): FilterFieldMeta[] | undefined {
    const normalized = entityName.trim()
    if (!normalized) return undefined
    return metaFiltersValueCache.get(normalized)
}

export function readCachedMetaEntity(entityName: string): MetadataEntityResponse | undefined {
    const normalized = entityName.trim()
    if (!normalized) return undefined
    return metaEntityValueCache.get(normalized)
}

function setCachedListViews(entityType: string, views: ListView[]): void {
    const normalized = normalizeListViewEntityType(entityType)
    listViewsValueCache.set(normalized, views)
    listViewsCache.set(normalized, Promise.resolve(views))
}

function updateCachedListViews(updater: (views: ListView[], entityType: string) => ListView[]): void {
    for (const [entityType, views] of listViewsValueCache.entries()) {
        setCachedListViews(entityType, updater(views, entityType))
    }
}

export function getCachedListViews(entityType: string): Promise<ListView[]> {
    const generation = getSessionGeneration()
    const normalized = normalizeListViewEntityType(entityType)
    const value = listViewsValueCache.get(normalized)
    if (value) return Promise.resolve(value)

    const cached = listViewsCache.get(normalized)
    if (cached) return cached

    const request = checkedJSON(
        `/me/list-views/${encodeURIComponent(normalized)}`, listViewSchema.array()
    ).then((views) => {
        assertCurrentSession(generation)
        listViewsValueCache.set(normalized, views)
        return views
    }).catch((err) => {
        assertCurrentSession(generation)
        listViewsCache.delete(normalized)
        throw err
    })

    listViewsCache.set(normalized, request)
    return request
}

export function readCachedListViews(entityType: string): ListView[] | undefined {
    const normalized = entityType.trim()
    if (!normalized) return undefined
    return listViewsValueCache.get(normalized)
}

// ── Meta APIs ─────────────────────────────────────────────────────────

export const metaApi = {
    listEntities: () =>
        apiFetch<JsonValue>("/meta/entities").then(value => z.array(entitySummarySchema).parse(value)),
    getFilters: getCachedMetaFilters,
    getEntity: getCachedMetaEntity,
    getMock: (entityName: string) =>
        apiFetch<JsonValue>(
            `/meta/${encodeURIComponent(entityName)}/mock`
        ).then(value => jsonObjectSchema.parse(value)),
}

export const preferencesApi = {
    get: () =>
        apiFetch("/me/preferences").then(value => preferencesSchema.parse(value)),

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

    saveListColumns: (entityType: string, data: string[] | Record<string, number>) =>
        apiFetch<void>(`/me/preferences/list-columns/${entityType}`, {
            method: "PUT",
            body: JSON.stringify(data),
        }),

    saveDashboardLayout: (layout: import("@/types/dashboard").DashboardLayout) =>
        apiFetch<void>("/me/preferences/dashboard-layout", {
            method: "PUT",
            body: JSON.stringify(layout),
        }),

    saveFavorites: (items: import("@/types/user-prefs").FavoriteItem[]) =>
        apiFetch<void>("/me/preferences/favorites", {
            method: "PUT",
            body: JSON.stringify(items),
        }),
}

export const listViewsApi = {
    list: getCachedListViews,

    create: async (data: import("@/types/list-view").CreateListViewRequest) => {
        const generation = getSessionGeneration()
        const created = await checkedJSON("/me/list-views", listViewSchema, {
            method: "POST",
            body: JSON.stringify(createListViewSchema.parse(data)),
        })
        assertCurrentSession(generation)
        const cached = readCachedListViews(created.entityType)
        if (cached) setCachedListViews(created.entityType, [...cached, created])
        return created
    },

    update: async (id: string, data: import("@/types/list-view").UpdateListViewRequest) => {
        const generation = getSessionGeneration()
        const updated = await checkedJSON(`/me/list-views/${id}`, listViewSchema, {
            method: "PUT",
            body: JSON.stringify(updateListViewSchema.parse(data)),
        })
        assertCurrentSession(generation)
        updateCachedListViews((views, entityType) => {
            const hasUpdatedView = views.some((view) => view.id === updated.id)
            if (!hasUpdatedView && entityType !== updated.entityType) return views
            return views.map((view) => view.id === updated.id ? updated : view)
        })
        return updated
    },

    delete: async (id: string) => {
        const generation = getSessionGeneration()
        await apiFetch<void>(`/me/list-views/${id}`, { method: "DELETE" })
        assertCurrentSession(generation)
        updateCachedListViews((views) => views.filter((view) => view.id !== id))
    },

    setDefault: async (id: string) => {
        const generation = getSessionGeneration()
        await apiFetch<void>(`/me/list-views/${id}/default`, { method: "PUT" })
        assertCurrentSession(generation)
        updateCachedListViews((views) => {
            if (!views.some((view) => view.id === id)) return views
            return views.map((view) => ({ ...view, isDefault: view.id === id }))
        })
    },
}

export const reportsApi = {
    variants: {
        list: (datasetKey: string) =>
            apiFetch<import("@/types/report-variant").ReportVariant[]>(`/reports/${datasetKey}/variants`).then(value => reportVariantSchema.array().parse(value)),
        create: (data: import("@/types/report-variant").CreateVariantRequest) =>
            apiFetch<import("@/types/report-variant").ReportVariant>("/reports/variants", {
                method: "POST",
                body: JSON.stringify({ ...data, config: variantConfigSchema.parse(data.config) }),
            }).then(value => reportVariantSchema.parse(value)),
        update: (id: string, data: import("@/types/report-variant").UpdateVariantRequest) =>
            apiFetch<void>(`/reports/variants/${id}`, {
                method: "PUT",
                body: JSON.stringify({ ...data, config: variantConfigSchema.parse(data.config) }),
            }),
        delete: (id: string) =>
            apiFetch<void>(`/reports/variants/${id}`, {
                method: "DELETE",
            }),
    },
    getDocumentJournal: async (params?: { fromDate?: string; toDate?: string; documentType?: string[]; posted?: boolean; limit?: number }) => {
        const filters: JsonObject = {}
        if (params?.fromDate) filters.from_date = params.fromDate
        if (params?.toDate) filters.to_date = params.toDate
        if (params?.posted !== undefined) filters.posted = params.posted

        const res = documentJournalResultSchema.parse(await apiFetch(`/reports/document-journal`, {
            method: "POST",
            body: JSON.stringify({
                dataset: "document-journal",
                limit: params?.limit,
                filters,
                advancedFilters: params?.documentType?.length ? [{ field: "document_type", operator: "in", value: params.documentType }] : undefined,
                select: [
                    "id",
                    "document_type",
                    "number",
                    "date",
                    "posted",
                    "counterparty_name",
                    "total_amount",
                    "currency",
                    "description"
                ]
            }),
        }))

        const items: DocumentJournalItem[] = res.items.map((i) => ({
            id: i.id,
            documentType: i.document_type,
            number: i.number,
            date: i.date,
            posted: i.posted,
            counterpartyName: i.counterparty_name ?? undefined,
            totalAmount: i.total_amount == null ? null : rawUnitsSchema.parse(i.total_amount),
            currency: i.currency ?? "",
            currencyId: i.currency_id ?? null,
            decimalPlaces: i.currency_decimal_places ?? null,
            description: i.description ?? undefined,
            createdAt: i.date,
            updatedAt: i.date,
        }))

        return {
            items,
            totalCount: res.totalItems,
            limit: params?.limit || 0,
            offset: 0,
        } satisfies DocumentJournalResponse
    },
}

export const settingsApi = {
    get: () =>
        apiFetch("/settings").then(value => systemSettingsSchema.parse(value)),
    updateSection: <S extends SettingsSection>(section: S, data: import("@/types/settings").SystemSettings[S], version: number) =>
        apiFetch(`/settings/${section}`, {
            method: "PATCH",
            body: JSON.stringify({ data, version }),
        }).then(value => systemSettingsSchema.parse(value)),
}

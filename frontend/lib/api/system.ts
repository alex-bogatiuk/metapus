import * as wireSchemas from "@/lib/generated/http-contract-schemas"
import { apiFetch, ApiError, API_BASE, TENANT_ID } from "./core"
import type { TenantListResponse, TenantSummary } from "./meta"

export const searchApi = {
    query: (q: string, limit?: number) =>
        apiFetch<import("@/lib/json-value").JsonValue>(
            `/search?q=${encodeURIComponent(q)}&limit=${limit ?? 5}`
        ).then(value => wireSchemas.SearchResponseSchema.parse(value)),
    preview: (entityType: string, entityKey: string, id: string) =>
        apiFetch<import("@/lib/json-value").JsonValue>(
            `/search/preview?entityType=${encodeURIComponent(entityType)}&entityKey=${encodeURIComponent(entityKey)}&id=${encodeURIComponent(id)}`
        ).then(value => wireSchemas.PreviewResponseSchema.parse(value)),
}

export const systemApi = {
    /** Public endpoint — no auth/tenant required. */
    version: async (): Promise<{ version: string; buildTime: string; expectedSchemaVersion: number }> => {
        const res = await fetch(`${API_BASE}/system/version`, {
            headers: TENANT_ID ? { "X-Tenant-ID": TENANT_ID } : {},
        })
        if (!res.ok) throw new ApiError(res.status, res.statusText)
        return wireSchemas.SystemVersionResponseSchema.parse(await res.json())
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

    workerJobs: {
        list: (params?: import("@/types/worker-job").WorkerJobFilter) => {
            const entries: [string, string][] = []
            if (params?.jobName)     entries.push(["jobName", params.jobName])
            if (params?.jobCategory) entries.push(["jobCategory", params.jobCategory])
            if (params?.status)      entries.push(["status", params.status])
            if (params?.dateFrom)    entries.push(["dateFrom", params.dateFrom])
            if (params?.dateTo)      entries.push(["dateTo", params.dateTo])
            if (params?.after)       entries.push(["after", params.after])
            if (params?.limit)       entries.push(["limit", String(params.limit)])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return apiFetch<import("@/types/worker-job").WorkerJobListResponse>(
                `/system/worker-jobs${qs}`
            )
        },
        stats: () =>
            apiFetch<import("@/types/worker-job").WorkerJobStats>("/system/worker-jobs/stats"),
    },

    findReferences: (data: { entityName: string; entityId: string }) =>
        apiFetch<{ items: import("@/types/common").FoundReference[]; total: number }>("/system/find-references", {
            method: "POST",
            body: JSON.stringify(data),
        }),

    markedObjects: {
        /** Deletion plan with every marked object selected. */
        list: () =>
            apiFetch<import("@/types/common").DeletionPlan>("/system/marked-objects"),
        /** Deletion plan for the given selection. */
        plan: (items: import("@/types/common").MarkedObjectRef[]) =>
            apiFetch<import("@/types/common").DeletionPlan>("/system/marked-objects/plan", {
                method: "POST",
                body: JSON.stringify({ items }),
            }),
        delete: (items: import("@/types/common").MarkedObjectRef[]) =>
            apiFetch<import("@/types/common").DeleteMarkedResult>("/system/marked-objects/delete", {
                method: "POST",
                body: JSON.stringify({ items }),
            }),
    },
    notifications: {
        list: (params?: { limit?: number; offset?: number; unreadOnly?: boolean }) => {
            const qs = new URLSearchParams()
            if (params?.limit) qs.set("limit", params.limit.toString())
            if (params?.offset) qs.set("offset", params.offset.toString())
            if (params?.unreadOnly) qs.set("unreadOnly", "true")
            return apiFetch<import("@/types/notification").NotificationListResponse>(`/system/notifications?${qs.toString()}`)
        },
        markAsRead: (id: string) =>
            apiFetch<void>(`/system/notifications/${id}/read`, { method: "PUT" }),
        markAsUnread: (id: string) =>
            apiFetch<void>(`/system/notifications/${id}/unread`, { method: "PUT" }),
        delete: (id: string) =>
            apiFetch<void>(`/system/notifications/${id}`, { method: "DELETE" }),
        markAllAsRead: () =>
            apiFetch<void>(`/system/notifications/mark-all-read`, { method: "PUT" }),
    },
}

export const adminApi = {
    tenants: {
        list: () =>
            apiFetch<TenantListResponse>("/admin/tenants"),
        get: (id: string) =>
            apiFetch<TenantSummary>(`/admin/tenants/${id}`),
    },
}

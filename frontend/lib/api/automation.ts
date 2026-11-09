import { z } from "zod"
import {
    automationRuleSchema,
    automationAccountSchema,
    automationChannelSchema,
    automationHistorySchema,
} from "@/lib/schemas/automation"
import { apiFetch } from "./core"

export const automationApi = {
    accounts: {
        list: () =>
            apiFetch("/system/automation-accounts").then(value => automationAccountSchema.array().parse(value)),
        get: (id: string) =>
            apiFetch(`/system/automation-accounts/${id}`).then(value => automationAccountSchema.parse(value)),
        create: (data: import("@/types/automation").CreateAccountRequest) =>
            apiFetch<{ id: string }>("/system/automation-accounts", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: import("@/types/automation").UpdateAccountRequest) =>
            apiFetch(`/system/automation-accounts/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }).then(value => automationAccountSchema.parse(value)),
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
    channels: {
        list: (accountId?: string) => {
            const qs = accountId ? `?accountId=${encodeURIComponent(accountId)}` : ""
            return apiFetch(`/system/automation-channels${qs}`).then(value => automationChannelSchema.array().parse(value))
        },
        get: (id: string) =>
            apiFetch(`/system/automation-channels/${id}`).then(value => automationChannelSchema.parse(value)),
        create: (data: import("@/types/automation").CreateChannelRequest) =>
            apiFetch<{ id: string }>("/system/automation-channels", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: import("@/types/automation").UpdateChannelRequest) =>
            apiFetch(`/system/automation-channels/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }).then(value => automationChannelSchema.parse(value)),
        delete: (id: string) =>
            apiFetch<void>(`/system/automation-channels/${id}`, { method: "DELETE" }),
    },
    rules: {
        list: (eventType?: string) => {
            const qs = eventType ? `?eventType=${encodeURIComponent(eventType)}` : ""
            return apiFetch(`/system/automation-rules${qs}`).then(value => z.array(automationRuleSchema).parse(value))
        },
        get: (id: string) =>
            apiFetch(`/system/automation-rules/${id}`).then(value => automationRuleSchema.parse(value)),
        create: (data: import("@/types/automation").CreateRuleRequest) =>
            apiFetch<{ id: string }>("/system/automation-rules", {
                method: "POST",
                body: JSON.stringify(data),
            }),
        update: (id: string, data: import("@/types/automation").UpdateRuleRequest) =>
            apiFetch(`/system/automation-rules/${id}`, {
                method: "PUT",
                body: JSON.stringify(data),
            }).then(value => automationRuleSchema.parse(value)),
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
            return apiFetch(`/system/automation-history${qs}`).then(value => z.object({ items: automationHistorySchema.array(), total: z.int() }).parse(value))
        },
        get: (id: string) =>
            apiFetch(`/system/automation-history/${id}`).then(value => automationHistorySchema.parse(value)),
        replay: (id: string) =>
            apiFetch<{ id: string }>(`/system/automation-history/${id}/replay`, { method: "POST" }),
        stats: (params?: { ruleId?: string; channelId?: string; from?: string; to?: string }) => {
            const entries: [string, string][] = []
            if (params?.ruleId) entries.push(["ruleId", params.ruleId])
            if (params?.channelId) entries.push(["channelId", params.channelId])
            if (params?.from) entries.push(["from", params.from])
            if (params?.to) entries.push(["to", params.to])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return apiFetch<import("@/types/automation").HistoryStatsResponse>(`/system/automation-history/stats${qs}`)
        },
        batchReplay: (params?: { ruleId?: string; channelId?: string }) => {
            const entries: [string, string][] = []
            if (params?.ruleId) entries.push(["ruleId", params.ruleId])
            if (params?.channelId) entries.push(["channelId", params.channelId])
            const qs = entries.length > 0 ? "?" + new URLSearchParams(entries).toString() : ""
            return apiFetch<{ queued: number }>(`/system/automation-history/batch-replay${qs}`, { method: "POST" })
        },
    },
    meta: {
        get: () =>
            apiFetch<import("@/types/automation").AutomationMeta>("/system/automation-meta"),
        datasets: () =>
            apiFetch<{ key: string; name: string }[]>("/system/automation-meta/datasets"),
    },
}

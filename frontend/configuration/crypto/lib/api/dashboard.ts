import "./contracts" // registers the configuration's route contracts before any request
import { apiFetch } from "@/lib/api/core"
import { AdminDashboardQueuesResponseSchema, AdminDashboardFinanceResponseSchema, AdminDashboardAnalyticsResponseSchema } from "@/configuration/crypto/lib/generated/http-contract-schemas"

export const dashboardApi = {
  queues: (kind = "all", limit = 5) => apiFetch(`/admin/dashboard/queues?${new URLSearchParams({ kind, limit: String(limit) })}`).then(v => AdminDashboardQueuesResponseSchema.parse(v)),
  finance: () => apiFetch("/admin/dashboard/finance").then(v => AdminDashboardFinanceResponseSchema.parse(v)),
  analytics: (period = "30d") => apiFetch(`/admin/dashboard/analytics?period=${encodeURIComponent(period)}`).then(v => AdminDashboardAnalyticsResponseSchema.parse(v)),
}

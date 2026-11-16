// Config schemas of the crypto-processing dashboard widgets.
import { z } from "zod"

export const counterConfigSchema = z.object({ metric: z.enum(["withdrawals", "inbound", "outflows", "networks"]) })
export const queueConfigSchema = z.object({ kind: z.enum(["all", "withdrawal", "inbound", "outflow"]).default("all"), limit: z.int().min(1).max(20).default(5) })
export const healthConfigSchema = z.object({ showWorkerStats: z.boolean().default(true) })
export const financeConfigSchema = z.object({ view: z.enum(["obligations", "wallets"]).default("obligations") })
export const reconciliationConfigSchema = z.object({ limit: z.int().min(1).max(20).default(8) })
export const analyticsConfigSchema = z.object({ period: z.enum(["7d", "30d", "90d"]).default("30d"), tokenId: z.string().default("") })

export type CounterConfig = z.infer<typeof counterConfigSchema>
export type QueueConfig = z.infer<typeof queueConfigSchema>
export type HealthConfig = z.infer<typeof healthConfigSchema>
export type FinanceConfig = z.infer<typeof financeConfigSchema>
export type ReconciliationConfig = z.infer<typeof reconciliationConfigSchema>
export type AnalyticsConfig = z.infer<typeof analyticsConfigSchema>

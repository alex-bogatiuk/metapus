import { z } from "zod"
import type { AutomationRule, AutomationAccount, AutomationChannel, AutomationHistoryEntry, ReportActionConfig } from "@/types/automation"
import * as wire from "@/lib/generated/http-contract-schemas"

const optionalText = z.string().nullish().transform(value => value ?? null)
const optionalID = z.uuid().nullish().transform(value => value ?? null)
const accountType = z.enum(["telegram", "email", "webhook", "rocketchat", "slack"])
export const automationAccountSchema = wire.AutomationAccountResponseSchema.transform((value): AutomationAccount => ({
  ...value, accountType: accountType.parse(value.accountType), status: z.enum(["active", "error", "disabled"]).parse(value.status),
  organizationId: value.organizationId ?? null, lastError: value.lastError ?? null, lastSuccessAt: value.lastSuccessAt ?? null,
}))
export const automationChannelSchema = wire.AutomationChannelResponseSchema.transform((value): AutomationChannel => ({
  ...value, accountType: value.accountType === undefined ? undefined : accountType.parse(value.accountType),
}))
export const automationHistorySchema = wire.AutomationHistoryResponseSchema.transform((value): AutomationHistoryEntry => ({
  ...value, status: z.enum(["success", "error", "condition_false", "skipped", "pending"]).parse(value.status),
  aggregateId: value.aggregateId ?? null, aggregateName: value.aggregateName ?? null, channelId: value.channelId ?? null,
  channelName: value.channelName ?? null, accountName: value.accountName ?? null, renderedPayload: value.renderedPayload ?? null,
  errorText: value.errorText ?? null, durationMs: value.durationMs ?? null,
}))
export const subscriberTypeSchema = z.enum(["channel", "user", "role", "doc_field"])

const reportConfigSchema: z.ZodType<ReportActionConfig> = z.object({
  executorUserId: z.uuid().optional(),
  datasetKey: z.string(),
  variantId: z.uuid().nullish(),
  periodType: z.enum(["today", "yesterday", "current_week", "last_week", "current_month", "last_month", "as_of_now", "custom_days"]),
  customDays: z.int().optional(),
  timezone: z.string().optional(),
  includeEmptyReport: z.boolean().optional(),
})

// The Go facade omits nil pointers; normalize those optional fields for forms.
// Required fields and closed enums are validated, never guessed from casts.
export const automationRuleSchema: z.ZodType<AutomationRule> = z.object({
  id: z.uuid(), name: z.string(), description: optionalText,
  triggerType: z.enum(["entity_event", "business_event", "scheduled", "incoming_webhook"]),
  eventType: z.string(), targetEntities: z.array(z.string()), conditionCel: optionalText,
  reactionType: z.enum(["notify", "webhook_call", "generate_report"]),
  notifSeverity: z.string().optional().default("info"),
  messageFormat: z.enum(["text", "markdown", "html"]), actionTemplate: z.string(),
  priority: z.int(), maxRetries: z.int(), cooldownSeconds: z.int(),
  organizationId: optionalID, isActive: z.boolean(), executionCount: z.int(), errorCount: z.int(),
  lastExecutedAt: optionalText,
  subscribers: z.array(z.object({
    id: z.uuid(), ruleId: z.uuid(), subscriberType: subscriberTypeSchema,
    channelId: optionalID, userId: optionalID, roleName: optionalText, docFieldPath: optionalText,
    deliveryMethod: z.string(), idx: z.int(), channelName: z.string().optional(), userName: z.string().optional(),
  })),
  reportConfig: reportConfigSchema.nullish().transform(value => value ?? null),
  deletionMark: z.boolean(), version: z.int(), createdAt: z.string(), updatedAt: z.string(),
})

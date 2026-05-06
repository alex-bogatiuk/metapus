// ── Automation Accounts ──────────────────────────────────────────────────
// Synced with Go: internal/domain/automations/account.go

export type AccountType = "telegram" | "email" | "webhook" | "rocketchat" | "slack"

export type AccountStatus = "active" | "error" | "disabled"

export interface AutomationAccount {
  id: string
  name: string
  accountType: AccountType
  config: Record<string, unknown>
  organizationId: string | null
  isActive: boolean
  status: AccountStatus
  lastError: string | null
  lastSuccessAt: string | null
  channelCount: number
  deletionMark: boolean
  version: number
  createdAt: string
  updatedAt: string
}

export interface CreateAccountRequest {
  name: string
  accountType: AccountType
  config: Record<string, unknown>
  organizationId?: string | null
  isActive: boolean
  credentials?: string
}

export interface UpdateAccountRequest {
  name: string
  config: Record<string, unknown>
  organizationId?: string | null
  isActive: boolean
  version: number
}

// ── Automation Channels ─────────────────────────────────────────────────
// Synced with Go: internal/domain/automations/channel.go

export interface AutomationChannel {
  id: string
  name: string
  accountId: string
  destination: Record<string, unknown>
  isActive: boolean
  deletionMark: boolean
  version: number
  createdAt: string
  updatedAt: string
  // Denormalized from Account
  accountName: string
  accountType: AccountType
  // Stats
  ruleCount: number
}

export interface CreateChannelRequest {
  name: string
  accountId: string
  destination: Record<string, unknown>
  isActive: boolean
}

export interface UpdateChannelRequest {
  name: string
  accountId: string
  destination: Record<string, unknown>
  isActive: boolean
  version: number
}

// ── Automation Subscribers ──────────────────────────────────────────────
// Synced with Go: internal/domain/automations/subscriber.go

export type SubscriberType = "channel" | "user" | "role" | "doc_field"

export interface AutomationSubscriber {
  id: string
  ruleId: string
  subscriberType: SubscriberType
  channelId: string | null
  userId: string | null
  roleName: string | null
  docFieldPath: string | null
  deliveryMethod: string
  idx: number
  // Denormalized for display
  channelName?: string
  userName?: string
}

export interface SubscriberInput {
  subscriberType: SubscriberType
  channelId?: string | null
  userId?: string | null
  roleName?: string | null
  docFieldPath?: string | null
  deliveryMethod: string
  idx: number
}

// ── Automation Rules ────────────────────────────────────────────────────
// Synced with Go: internal/domain/automations/rule.go

export type TriggerType = "entity_event" | "business_event" | "scheduled" | "incoming_webhook"
export type ReactionType = "notify" | "webhook_call" | "chain" | "create_record" | "generate_report"
export type MessageFormat = "text" | "markdown" | "html"

export interface AutomationRule {
  id: string
  name: string
  description: string | null
  triggerType: TriggerType
  eventType: string
  targetEntities: string[]
  conditionCel: string | null
  reactionType: ReactionType
  notifSeverity: string
  messageFormat: MessageFormat
  actionTemplate: string
  chainRuleIds: string[]
  priority: number
  maxRetries: number
  cooldownSeconds: number
  organizationId: string | null
  isActive: boolean
  executionCount: number
  errorCount: number
  lastExecutedAt: string | null
  subscribers: AutomationSubscriber[]
  reportConfig: ReportActionConfig | null
  deletionMark: boolean
  version: number
  createdAt: string
  updatedAt: string
}

export interface CreateRuleRequest {
  name: string
  description?: string | null
  triggerType: TriggerType
  eventType: string
  targetEntities: string[]
  conditionCel?: string | null
  reactionType: ReactionType
  notifSeverity?: string
  messageFormat: MessageFormat
  actionTemplate: string
  chainRuleIds?: string[]
  priority: number
  maxRetries: number
  cooldownSeconds: number
  organizationId?: string | null
  isActive: boolean
  subscribers: SubscriberInput[]
  reportConfig?: ReportActionConfig | null
}

export interface UpdateRuleRequest extends CreateRuleRequest {
  version: number
}

// ── Report Action Config ────────────────────────────────────────────────
// Synced with Go: internal/domain/automations/report_config.go

export type PeriodType =
  | "today"
  | "yesterday"
  | "current_week"
  | "last_week"
  | "current_month"
  | "last_month"
  | "as_of_now"
  | "custom_days"

export interface ReportActionConfig {
  datasetKey: string
  variantId?: string | null
  periodType: PeriodType
  customDays?: number
  timezone?: string
  skipEmpty?: boolean
}

// ── Test Rule ───────────────────────────────────────────────────────────

export interface TestRuleRequest {
  conditionCel?: string | null
  actionTemplate: string
  payload: Record<string, unknown>
}

export interface TestRuleResponse {
  conditionMatched: boolean
  conditionError?: string
  renderedPayload?: string
  renderError?: string
}

// ── Automation History ──────────────────────────────────────────────────
// Synced with Go: internal/domain/automations/history.go

export type HistoryStatus = "success" | "error" | "condition_false" | "skipped" | "pending"

export interface AutomationHistoryEntry {
  id: string
  ruleId: string
  ruleName: string
  eventType: string
  aggregateId: string | null
  aggregateName: string | null
  status: HistoryStatus
  channelId: string | null
  channelName: string | null
  accountName: string | null
  renderedPayload: string | null
  errorText: string | null
  durationMs: number | null
  outboxEventId: string | null
  createdAt: string
}

export interface HistoryListResponse {
  items: AutomationHistoryEntry[]
  total: number
}

export interface HistoryStatsResponse {
  total: number
  byStatus: Partial<Record<HistoryStatus, number>>
}

// ── Automation Meta ─────────────────────────────────────────────────────

export interface EnumOption {
  value: string
  label: string
}

export interface EventTypeGroup {
  label: string
  items: EnumOption[]
}

export interface AutomationMeta {
  accountTypes: EnumOption[]
  triggerTypes: EnumOption[]
  reactionTypes: EnumOption[]
  subscriberTypes: EnumOption[]
  deliveryMethods: EnumOption[]
  messageFormats: EnumOption[]
  historyStatuses: EnumOption[]
  eventTypeGroups: EventTypeGroup[]
}

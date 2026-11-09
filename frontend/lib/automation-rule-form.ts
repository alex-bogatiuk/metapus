// Shared types and helpers for automation rule forms (new + edit).
// Eliminates duplication between new/page.tsx and [id]/page.tsx.

import type {
  TriggerType, ReactionType, MessageFormat, PeriodType,
  CreateRuleRequest, UpdateRuleRequest,
  SubscriberInput, AutomationRule, SubscriberType,
} from "@/types/automation"

// ── Form State ──────────────────────────────────────────────────────────

export interface RuleFormState {
  id?: string
  name: string
  description: string
  triggerType: TriggerType
  eventType: string
  targetEntities: string[]
  cronExpression: string
  reactionType: ReactionType
  notifSeverity: string
  conditionCel: string
  actionTemplate: string
  isActive: boolean
  messageFormat: MessageFormat
  priority: number
  maxRetries: number
  cooldownSeconds: number
  version: number
  subscribers: SubscriberFormEntry[]
  // Report generation fields (only used when reactionType === 'generate_report')
  reportDatasetKey: string
  reportVariantId: string
  reportPeriodType: PeriodType
  reportCustomDays: number
  reportTimezone: string
  reportSkipEmpty: boolean
}

/** Inline subscriber entry in the form (simplified for UI). */
export interface SubscriberFormEntry {
  subscriberType: SubscriberType
  channelId?: string
  userId?: string
  roleName?: string
  docFieldPath?: string
  deliveryMethod: string
  /** Display name resolved from channels/users list */
  displayName?: string
}

export const INITIAL_RULE_STATE: RuleFormState = {
  name: "",
  description: "",
  triggerType: "entity_event",
  eventType: "posted",
  targetEntities: [],
  cronExpression: "0 0 9 * * *",
  reactionType: "notify",
  notifSeverity: "info",
  conditionCel: "doc.totalAmount > 0",
  actionTemplate: 'Проведение документа #{{ .doc.number }}, сумма: {{ .doc.totalAmount }}',
  isActive: true,
  messageFormat: "text",
  priority: 50,
  maxRetries: 3,
  cooldownSeconds: 0,
  version: 1,
  subscribers: [],
  reportDatasetKey: "",
  reportVariantId: "",
  reportPeriodType: "today",
  reportCustomDays: 7,
  reportTimezone: "",
  reportSkipEmpty: true,
}

// ── Trigger Type Options ────────────────────────────────────────────────

export interface TriggerTypeOption {
  value: TriggerType
  label: string
  description: string
  icon: string // lucide icon name for reference
}

export const TRIGGER_TYPE_OPTIONS: TriggerTypeOption[] = [
  {
    value: "entity_event",
    label: "По событию документа",
    description: "Проведение, создание, удаление",
    icon: "FileText",
  },
  {
    value: "business_event",
    label: "Бизнес-событие",
    description: "Загрузка курсов, пересчёт остатков",
    icon: "Activity",
  },
  {
    value: "scheduled",
    label: "По расписанию",
    description: "Настройте расписание запуска",
    icon: "Clock",
  },
]

// ── Entity Event Actions ────────────────────────────────────────────────
// Actions for entity_event trigger type.
// Entity list comes dynamically from useMetadataStore().getEntitiesByType("document").

export interface EntityEventAction {
  value: string
  label: string
}

export const ENTITY_EVENT_ACTIONS: EntityEventAction[] = [
  { value: "posted",          label: "Проведение документа" },
  { value: "unposted",        label: "Отмена проведения" },
  { value: "created",         label: "Создание" },
  { value: "updated",         label: "Изменение" },
  { value: "deleted",         label: "Удаление" },
  { value: "deletion_marked", label: "Пометка на удаление" },
]

/** Get human-readable label for an entity event action. */
export function getActionLabel(action: string): string {
  return ENTITY_EVENT_ACTIONS.find(a => a.value === action)?.label ?? action
}

// ── Severity Options ────────────────────────────────────────────────────

export interface SeverityOption {
  value: string
  label: string
  description: string
}

export const SEVERITY_OPTIONS: SeverityOption[] = [
  { value: "info",    label: "Информация",    description: "Стандартное уведомление" },
  { value: "warning", label: "Предупреждение", description: "Требует внимания" },
  { value: "error",   label: "Ошибка",        description: "Критическое событие" },
  { value: "success", label: "Успех",         description: "Операция выполнена" },
]

// ── Period Type Options (for generate_report reaction) ──────────────────

export interface PeriodTypeOption {
  value: PeriodType
  label: string
}

export const PERIOD_TYPE_OPTIONS: PeriodTypeOption[] = [
  { value: "today",         label: "Сегодня" },
  { value: "yesterday",     label: "Вчера" },
  { value: "current_week",  label: "Текущая неделя" },
  { value: "last_week",     label: "Прошлая неделя" },
  { value: "current_month", label: "Текущий месяц" },
  { value: "last_month",    label: "Прошлый месяц" },
  { value: "as_of_now",     label: "На текущий момент" },
  { value: "custom_days",   label: "Последние N дней" },
]

// ── Mappers ──────────────────────────────────────────────────────────────

function buildSubscribers(entries: SubscriberFormEntry[]): SubscriberInput[] {
  return entries.map((e, idx) => ({
    subscriberType: e.subscriberType,
    channelId: e.channelId ?? null,
    userId: e.userId ?? null,
    roleName: e.roleName ?? null,
    docFieldPath: e.docFieldPath ?? null,
    deliveryMethod: e.deliveryMethod,
    idx: idx + 1,
  }))
}

/** Resolve eventType for API: scheduled rules use "cron:<expression>". */
function resolveEventType(s: RuleFormState): string {
  if (s.triggerType === "scheduled") {
    return `cron:${s.cronExpression}`
  }
  return s.eventType
}

export function mapRuleToCreate(s: RuleFormState): CreateRuleRequest {
  const base: CreateRuleRequest = {
    name: s.name,
    description: s.description || undefined,
    triggerType: s.triggerType,
    eventType: resolveEventType(s),
    targetEntities: s.targetEntities,
    reactionType: s.reactionType,
    notifSeverity: s.notifSeverity || undefined,
    conditionCel: s.triggerType !== "scheduled" ? (s.conditionCel || undefined) : undefined,
    actionTemplate: s.actionTemplate,
    isActive: s.isActive,
    messageFormat: s.messageFormat,
    priority: s.priority,
    maxRetries: s.maxRetries,
    cooldownSeconds: s.cooldownSeconds,
    subscribers: buildSubscribers(s.subscribers),
  }

  if (s.reactionType === "generate_report" && s.reportDatasetKey) {
    base.reportConfig = {
      datasetKey: s.reportDatasetKey,
      variantId: s.reportVariantId || undefined,
      periodType: s.reportPeriodType,
      customDays: s.reportPeriodType === "custom_days" ? s.reportCustomDays : undefined,
      timezone: s.reportTimezone || undefined,
      includeEmptyReport: !s.reportSkipEmpty,
    }
  }

  return base
}

export function mapRuleToUpdate(s: RuleFormState): UpdateRuleRequest {
  return {
    ...mapRuleToCreate(s),
    version: s.version,
  }
}

export function mapRuleFromResponse(r: AutomationRule): RuleFormState {
  const subscribers = r.subscribers
  const entries: SubscriberFormEntry[] = subscribers.map(s => ({
    subscriberType: s.subscriberType,
    channelId: s.channelId ?? undefined,
    userId: s.userId ?? undefined,
    roleName: s.roleName ?? undefined,
    docFieldPath: s.docFieldPath ?? undefined,
    deliveryMethod: s.deliveryMethod,
    displayName: s.channelName || s.userName || s.roleName || s.docFieldPath || "",
  }))

  const eventType = r.eventType
  const triggerType = r.triggerType
  const targetEntities = r.targetEntities

  // Parse CRON from eventType for scheduled rules
  let cronExpression = "0 0 9 * * *"
  if (triggerType === "scheduled" && eventType.startsWith("cron:")) {
    cronExpression = eventType.slice(5) // strip "cron:"
  }

  // Extract report config (may be null)
  const rc = r.reportConfig

  return {
    id: r.id,
    name: r.name,
    description: r.description ?? "",
    triggerType,
    eventType: triggerType === "scheduled" ? "" : eventType,
    targetEntities,
    cronExpression,
    reactionType: r.reactionType,
    notifSeverity: r.notifSeverity,
    conditionCel: r.conditionCel ?? "",
    actionTemplate: r.actionTemplate,
    isActive: r.isActive,
    messageFormat: r.messageFormat,
    priority: r.priority,
    maxRetries: r.maxRetries,
    cooldownSeconds: r.cooldownSeconds,
    version: r.version,
    subscribers: entries,
    // Report config fields
    reportDatasetKey: rc?.datasetKey || "",
    reportVariantId: rc?.variantId || "",
    reportPeriodType: rc?.periodType ?? "today",
    reportCustomDays: rc?.customDays ?? 7,
    reportTimezone: rc?.timezone || "",
    reportSkipEmpty: !rc?.includeEmptyReport,
  }
}

export function validateRule(s: RuleFormState): string | null {
  if (!s.name) return "Укажите наименование"

  if (s.triggerType === "scheduled") {
    if (!s.cronExpression) return "Настройте расписание"
  } else if (s.triggerType === "entity_event") {
    if (!s.eventType) return "Укажите событие"
  } else {
    if (!s.eventType) return "Укажите тип события"
  }

  if (!s.reactionType) return "Укажите тип действия"

  if (s.reactionType === "generate_report") {
    if (!s.reportDatasetKey) return "Выберите набор данных для отчёта"
    if (s.reportPeriodType === "custom_days" && (!s.reportCustomDays || s.reportCustomDays < 1)) {
      return "Укажите количество дней для периода"
    }
  }

  if (s.subscribers.length === 0) return "Добавьте хотя бы одного подписчика"
  return null
}

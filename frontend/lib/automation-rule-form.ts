// Shared types and helpers for automation rule forms (new + edit).
// Eliminates duplication between new/page.tsx and [id]/page.tsx.

import type {
  TriggerType, ReactionType, MessageFormat,
  CreateRuleRequest, UpdateRuleRequest,
  SubscriberInput,
} from "@/types/automation"

// ── Form State ──────────────────────────────────────────────────────────

export interface RuleFormState {
  // Index signature required by useCatalogForm<TState extends Record<string, unknown>>
  [key: string]: string | number | boolean | string[] | SubscriberFormEntry[] | undefined
  id?: string
  name: string
  description: string
  triggerType: TriggerType
  eventType: string
  targetEntities: string[]
  cronExpression: string
  reactionType: ReactionType
  conditionCel: string
  actionTemplate: string
  isActive: boolean
  messageFormat: MessageFormat
  priority: number
  maxRetries: number
  cooldownSeconds: number
  version: number
  subscribers: SubscriberFormEntry[]
}

/** Inline subscriber entry in the form (simplified for UI). */
export interface SubscriberFormEntry {
  subscriberType: "channel" | "user" | "role"
  channelId?: string
  userId?: string
  roleName?: string
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
  conditionCel: "doc.totalAmount > 0",
  actionTemplate: 'Проведение документа #{{ .doc.number }}, сумма: {{ .doc.totalAmount }}',
  isActive: true,
  messageFormat: "text",
  priority: 50,
  maxRetries: 3,
  cooldownSeconds: 0,
  version: 1,
  subscribers: [],
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

// ── Mappers ──────────────────────────────────────────────────────────────

function buildSubscribers(entries: SubscriberFormEntry[]): SubscriberInput[] {
  return entries.map((e, idx) => ({
    subscriberType: e.subscriberType,
    channelId: e.channelId ?? null,
    userId: e.userId ?? null,
    roleName: e.roleName ?? null,
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
  return {
    name: s.name,
    description: s.description || undefined,
    triggerType: s.triggerType,
    eventType: resolveEventType(s),
    targetEntities: s.targetEntities,
    reactionType: s.reactionType,
    conditionCel: s.triggerType !== "scheduled" ? (s.conditionCel || undefined) : undefined,
    actionTemplate: s.actionTemplate,
    isActive: s.isActive,
    messageFormat: s.messageFormat,
    priority: s.priority,
    maxRetries: s.maxRetries,
    cooldownSeconds: s.cooldownSeconds,
    subscribers: buildSubscribers(s.subscribers),
  }
}

export function mapRuleToUpdate(s: RuleFormState): UpdateRuleRequest {
  return {
    ...mapRuleToCreate(s),
    version: s.version,
  }
}

export function mapRuleFromResponse(r: Record<string, unknown>): RuleFormState {
  const subscribers = (r.subscribers as Array<Record<string, unknown>>) || []
  const entries: SubscriberFormEntry[] = subscribers.map(s => ({
    subscriberType: (s.subscriberType as "channel" | "user" | "role") || "channel",
    channelId: s.channelId as string | undefined,
    userId: s.userId as string | undefined,
    roleName: s.roleName as string | undefined,
    deliveryMethod: (s.deliveryMethod as string) || "push",
    displayName: (s.channelName as string) || (s.userName as string) || (s.roleName as string) || "",
  }))

  const eventType = (r.eventType as string) || ""
  const triggerType = (r.triggerType as TriggerType) || "entity_event"
  const targetEntities = (r.targetEntities as string[]) ?? []

  // Parse CRON from eventType for scheduled rules
  let cronExpression = "0 0 9 * * *"
  if (triggerType === "scheduled" && eventType.startsWith("cron:")) {
    cronExpression = eventType.slice(5) // strip "cron:"
  }

  return {
    id: r.id as string,
    name: (r.name as string) || "",
    description: (r.description as string) || "",
    triggerType,
    eventType: triggerType === "scheduled" ? "" : eventType,
    targetEntities,
    cronExpression,
    reactionType: (r.reactionType as ReactionType) || "notify",
    conditionCel: (r.conditionCel as string) || "",
    actionTemplate: (r.actionTemplate as string) || "",
    isActive: (r.isActive as boolean) ?? true,
    messageFormat: (r.messageFormat as MessageFormat) || "text",
    priority: (r.priority as number) || 50,
    maxRetries: (r.maxRetries as number) || 3,
    cooldownSeconds: (r.cooldownSeconds as number) || 0,
    version: (r.version as number) || 1,
    subscribers: entries,
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
  if (s.subscribers.length === 0) return "Добавьте хотя бы одного подписчика"
  return null
}

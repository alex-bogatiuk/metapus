/** Single event log entry from API */
export interface EventLogEntry {
  id: string;
  category: EventCategory;
  severity: EventSeverity;
  eventType: string;
  source: string;
  sessionId?: string;
  userId?: string;
  userEmail?: string;
  clientIp?: string;
  entityType?: string;
  entityId?: string;
  entityNumber?: string;
  message: string;
  details?: Record<string, unknown>;
  traceId?: string;
  requestId?: string;
  durationMs?: number;
  createdAt: string;
}

export type EventCategory =
  | "session"
  | "data"
  | "security"
  | "background"
  | "system"
  | "api";

export type EventSeverity = "info" | "warning" | "error" | "critical";

/** Aggregated statistics */
export interface EventLogStats {
  total: number;
  info: number;
  warning: number;
  error: number;
  critical: number;
}

/** Filter parameters for the list API */
export interface EventLogFilter {
  category?: string;
  severity?: string;
  eventType?: string;
  userId?: string;
  entityType?: string;
  entityId?: string;
  entityNumber?: string;
  source?: string;
  search?: string;
  traceId?: string;
  dateFrom?: string;
  dateTo?: string;
  orderBy?: string;
}

/** Category metadata for UI filters */
export const EVENT_CATEGORIES: { value: EventCategory; label: string }[] = [
  { value: "session", label: "Сессии" },
  { value: "data", label: "Данные" },
  { value: "security", label: "Безопасность" },
  { value: "background", label: "Фоновые" },
  { value: "system", label: "Система" },
  { value: "api", label: "API" },
];

/** Severity metadata for UI filters */
export const EVENT_SEVERITIES: {
  value: EventSeverity;
  label: string;
  color: string;
}[] = [
  { value: "info", label: "Информация", color: "text-blue-600" },
  { value: "warning", label: "Предупреждение", color: "text-yellow-600" },
  { value: "error", label: "Ошибка", color: "text-red-600" },
  { value: "critical", label: "Критическое", color: "text-red-800" },
];

/** Event type metadata for UI filters */
export const EVENT_TYPES: { value: string; label: string; category: EventCategory }[] = [
  // Session events
  { value: "session.login", label: "Вход в систему", category: "session" },
  { value: "session.login_failed", label: "Неудачный вход", category: "session" },
  { value: "session.logout", label: "Выход из системы", category: "session" },
  { value: "session.token_refresh", label: "Обновление токена", category: "session" },
  { value: "session.impersonate", label: "Имперсонация", category: "session" },
  { value: "session.brute_force", label: "Brute-force", category: "session" },
  // Data events — documents
  { value: "document.create", label: "Создание документа", category: "data" },
  { value: "document.update", label: "Изменение документа", category: "data" },
  { value: "document.delete", label: "Удаление документа", category: "data" },
  { value: "document.post", label: "Проведение", category: "data" },
  { value: "document.unpost", label: "Отмена проведения", category: "data" },
  // Data events — catalogs
  { value: "catalog.create", label: "Создание элемента", category: "data" },
  { value: "catalog.update", label: "Изменение элемента", category: "data" },
  { value: "catalog.delete", label: "Удаление элемента", category: "data" },
  // Security events
  { value: "security.permission_denied", label: "Доступ запрещён", category: "security" },
  { value: "security.rls_blocked", label: "RLS заблокировано", category: "security" },
  { value: "security.cel_denied", label: "CEL отказ", category: "security" },
  { value: "security.profile_changed", label: "Профиль безопасности изменён", category: "security" },
  // Business logic
  { value: "stock.negative_balance", label: "Отрицательный остаток", category: "system" },
  { value: "numerator.generated", label: "Генерация номера", category: "system" },
  // API events
  { value: "api.slow_request", label: "Медленный запрос", category: "api" },
  { value: "api.error_500", label: "Ошибка 500", category: "api" },
  { value: "api.rate_limited", label: "Rate limit", category: "api" },
  // System events
  { value: "system.migration", label: "Миграция", category: "system" },
  { value: "system.panic", label: "Паника", category: "system" },
  { value: "system.startup", label: "Запуск системы", category: "system" },
  { value: "system.shutdown", label: "Остановка системы", category: "system" },
];

/** Source metadata for UI filters */
export const EVENT_SOURCES: { value: string; label: string }[] = [
  { value: "api", label: "API" },
  { value: "background", label: "Фоновое задание" },
  { value: "system", label: "Система" },
];

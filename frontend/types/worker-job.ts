/** Single worker job execution run from API */
export interface WorkerJob {
  id: string;
  jobName: string;
  jobCategory: string;
  status: WorkerJobStatus;
  startedAt: string;
  finishedAt?: string;
  durationMs?: number;
  itemsProcessed?: number;
  errorMessage?: string;
}

export type WorkerJobStatus = "running" | "success" | "error" | "skipped";

/** Aggregated KPI stats for the last 24 hours */
export interface WorkerJobStats {
  total: number;
  success: number;
  error: number;
  avgDuration: number; // milliseconds
}

/** Paginated list response */
export interface WorkerJobListResponse {
  items: WorkerJob[];
  nextCursor?: string;
  hasMore: boolean;
  totalCount: number;
}

/** Filter options for the list UI */
export interface WorkerJobFilter {
  jobName?: string;
  jobCategory?: string;
  status?: string;
  dateFrom?: string;
  dateTo?: string;
  after?: string;
  limit?: number;
}

// ── UI Constants ──────────────────────────────────────────────────────────

export const JOB_STATUSES: { value: WorkerJobStatus; label: string; color: string }[] = [
  { value: "running",  label: "Выполняется", color: "text-blue-600" },
  { value: "success",  label: "Успешно",     color: "text-green-600" },
  { value: "skipped",  label: "Пропущено",   color: "text-muted-foreground" },
  { value: "error",    label: "Ошибка",      color: "text-red-600" },
]

export const JOB_CATEGORIES: { value: string; label: string }[] = [
  { value: "crypto",     label: "Криптовалюта" },
  { value: "outbox",     label: "Outbox" },
  { value: "cleanup",    label: "Очистка" },
  { value: "automation", label: "Автоматизация" },
]

export const KNOWN_JOB_NAMES: { value: string; label: string }[] = [
  { value: "outbox.relay",                label: "Outbox: отправка" },
  { value: "outbox.recover_stuck",        label: "Outbox: восстановление" },
  { value: "cleanup.sessions",            label: "Очистка: сессии" },
  { value: "cleanup.idempotency",         label: "Очистка: идемпотентность" },
  { value: "cleanup.automation_history",  label: "Очистка: история автоматизации" },
  { value: "cleanup.automation_files",    label: "Очистка: файлы автоматизации" },
  { value: "cleanup.notifications",       label: "Очистка: уведомления" },
  { value: "cleanup.worker_jobs",         label: "Очистка: логи задач" },
  { value: "crypto.expiration",           label: "Крипто: истечение инвойсов" },
  { value: "crypto.sweep_eval",           label: "Крипто: оценка свипа" },
]

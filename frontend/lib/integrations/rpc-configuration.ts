import type { CryptoRPCConfigurationResponse, UpdateCryptoRPCConfigurationRequest } from "@/types/generated/http-contracts"

export type RPCConnection = CryptoRPCConfigurationResponse["endpoints"][number]

export function orderRPCConnections(rows: RPCConnection[]): RPCConnection[] {
  return [...rows].sort((a, b) => a.role.localeCompare(b.role) || a.priority - b.priority || a.id.localeCompare(b.id))
}

export function moveRPCConnection(rows: RPCConnection[], id: string, delta: -1 | 1): RPCConnection[] {
  const selected = rows.find(row => row.id === id)
  if (!selected) return rows
  const group = orderRPCConnections(rows.filter(row => row.role === selected.role))
  const index = group.findIndex(row => row.id === id)
  if (index + delta < 0 || index + delta >= group.length) return rows
  ;[group[index], group[index + delta]] = [group[index + delta], group[index]]
  const priorities = new Map(group.map((row, position) => [row.id, (position + 1) * 10]))
  return orderRPCConnections(rows.map(row => priorities.has(row.id) ? { ...row, priority: priorities.get(row.id)! } : row))
}

export function rpcConfigurationChanges(before: RPCConnection[], after: RPCConnection[]): NonNullable<UpdateCryptoRPCConfigurationRequest["endpoints"]> {
  const baseline = new Map(before.map(row => [row.id, row]))
  return after.filter(row => {
    const old = baseline.get(row.id)
    return old && (old.isActive !== row.isActive || old.priority !== row.priority || old.minRequestIntervalMs !== row.minRequestIntervalMs)
  }).map(row => ({ id: row.id, version: row.version, isActive: row.isActive, priority: row.priority, ...(baseline.get(row.id)?.minRequestIntervalMs !== row.minRequestIntervalMs ? { minRequestIntervalMs: row.minRequestIntervalMs } : {}) }))
}

export function rpcCoverage(rows: RPCConnection[], requirements: CryptoRPCConfigurationResponse["requirements"]) {
  return requirements.map(requirement => ({
    ...requirement,
    configuredProviders: new Set(rows.filter(row => row.isActive && row.role === requirement.role).map(row => row.trustGroup)).size,
  }))
}

const reasons: Record<string, string> = {
  budget_unavailable: "Бюджета операции недостаточно для этого провайдера",
  rpc_unavailable: "Нет доступных источников для операции",
  timeout: "Провайдер не ответил вовремя",
  rate_limited: "Провайдер ограничил частоту запросов",
  unsupported: "Метод не поддерживается",
  archive_unavailable: "Архивные данные недоступны на тарифе",
  pool_exhausted: "Квота аккаунтов пула исчерпана",
  pool_unavailable: "Нет доступного аккаунта в пуле",
  pool_key_missing: "В аккаунтах пула нет ключа, который читает подключение",
  credentials_expired: "Срок действия ключа истёк",
  query_limit: "Провайдер ограничил диапазон запроса",
  invalid_credentials: "Ключ не принят провайдером",
  authentication: "Провайдер отклонил авторизацию",
  access_denied: "Доступ отклонён провайдером",
  head_lagging: "Узел отстаёт от сети",
  head_behind: "Запрошенный блок ещё недоступен на узле",
  stale_head: "Узел возвращает устаревший блок",
  invalid_header: "Некорректный заголовок блока",
  invalid_response: "Некорректный ответ",
  invalid_trace: "Некорректные данные операций",
  unsupported_trace: "Неподдерживаемый тип операции",
  response_limit: "Превышен допустимый размер ответа",
  http_error: "Ошибка HTTP у провайдера",
  rpc_error: "Ошибка RPC у провайдера",
  transport_or_decode: "Ошибка соединения или чтения ответа",
  configuration_unavailable: "Рабочая конфигурация недоступна",
  wrong_chain: "Узел подключён к другой сети",
  account_attempt_budget: "Достигнут предел попыток выбора аккаунта",
  decoder_version_conflict: "Требуется согласовать версию обработчика",
  admission_busy: "Достигнут лимит одновременных запросов",
  invalid_request: "Провайдер отклонил параметры запроса",
  canceled: "Запрос отменён",
  not_found: "Данные ещё не найдены",
}

export function rpcReasonLabel(code?: string): string {
  return code ? reasons[code] ?? "Причина не определена" : "Нет текущих ошибок"
}

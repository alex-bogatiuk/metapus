import type { CryptoNetworkState } from "@/types/crypto-network"

export function networkProblem(state: CryptoNetworkState, now = Date.now()): string | null {
  if (!state.policy.enabled) return null
  const runtime = state.runtime
  const age = now - Date.parse(runtime.observedAt)
  if (!Number.isFinite(age) || age > 180_000 || age < -60_000) return "Нет свежего состояния"
  if (runtime.operationalHold) return "Обработка приостановлена"
  if (!runtime.signerReady) return "Подпись недоступна"
  if (runtime.rpcEndpoints.length === 0 || runtime.rpcEndpoints.some(e => e.state !== "closed")) return "Проблема с RPC"
  if (state.policy.externalResources?.strategy === "rental_required" && !runtime.resourceProviderReady) return "Ресурсы недоступны"
  return null
}
export const queueKinds = { all: "Все очереди", withdrawal: "Заявки на вывод", inbound: "Дополнительные платежи", outflow: "Внешние списания" } as const
export const counterLabels = { withdrawals: "Заявки на вывод", inbound: "Платежи на проверке", outflows: "Внешние списания", networks: "Сети с проблемами" } as const
export const queueHref = (kind: string, id?: string) => kind === "withdrawal"
  ? (id ? `/documents/withdrawal-request/${id}` : "/documents/withdrawal-requests")
  : kind === "inbound" ? "/crypto/inbound-reviews" : `/documents/treasury-withdrawals?tab=reconciliation${id ? `&outflow=${id}` : ""}`
export function observedTime(value: string | null | undefined): string {
  if (!value || !Number.isFinite(Date.parse(value))) return "Нет данных"
  return new Date(value).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })
}

export const queueItemLabels = { withdrawal: "Заявка на вывод", inbound: "Дополнительный платёж", outflow: "Внешнее списание" } as const
export const queueStatusLabels: Record<string, string> = { pending_signature: "Ожидает подписи", pending_approval: "На согласовании", needs_review: "Нужна проверка", pending_review: "Нужна классификация", classified: "Ожидает решения" }

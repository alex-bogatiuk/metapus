import type { CryptoNetworkState } from "@/configuration/crypto/types/crypto-network"
import { networkHealth } from "../configuration/crypto/lib/crypto-network-readiness"

export { networkHealth } from "../configuration/crypto/lib/crypto-network-readiness"

/** Counted on the dashboard: networks that cannot process operations. */
export function networkProblem(state: CryptoNetworkState, now = Date.now()): string | null {
  const health = networkHealth(state, now)
  return health?.level === "problem" ? health.label : null
}
export const queueKinds = { all: "Все очереди", withdrawal: "Заявки на вывод", inbound: "Невыясненные поступления", outflow: "Внешние списания" } as const
export const counterLabels = { withdrawals: "Заявки на вывод", inbound: "Невыясненные поступления", outflows: "Внешние списания", networks: "Сети с проблемами" } as const
export const queueHref = (kind: string, id?: string) => kind === "withdrawal"
  ? (id ? `/documents/withdrawal-request/${id}` : "/documents/withdrawal-requests")
  : kind === "inbound" ? "/crypto/unidentified-receipts" : `/documents/treasury-withdrawals?tab=reconciliation${id ? `&outflow=${id}` : ""}`
export function observedTime(value: string | null | undefined): string {
  if (!value || !Number.isFinite(Date.parse(value))) return "Нет данных"
  return new Date(value).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })
}

export const queueItemLabels = { withdrawal: "Заявка на вывод", inbound: "Невыясненное поступление", outflow: "Внешнее списание" } as const
export const queueStatusLabels: Record<string, string> = { pending_signature: "Ожидает подписи", pending_approval: "На согласовании", needs_review: "Нужна проверка", unattributed: "Не зачислено", awaiting_approval: "Ждёт подтверждения", pending_review: "Нужна классификация", classified: "Ожидает решения" }

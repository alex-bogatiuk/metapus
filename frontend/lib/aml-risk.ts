import type { WithdrawalRiskClass } from "@/types/portal-api"

export const riskClassLabels: Record<WithdrawalRiskClass, string> = {
  low: "Низкий", medium: "Средний", high: "Высокий", unknown: "Неизвестный",
}

export function isRiskClass(value: string): value is WithdrawalRiskClass {
  return value === "low" || value === "medium" || value === "high" || value === "unknown"
}

export function formatRiskClass(value: string | null | undefined): string {
  return value && isRiskClass(value) ? riskClassLabels[value] : riskClassLabels.unknown
}

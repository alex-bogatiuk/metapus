/**
 * The crypto-processing configuration's display vocabulary for platform
 * screens: status colours, permission resource labels and worker jobs.
 */
import { registerResourceLabels } from "@/lib/security-registry"
import { registerStatusColors } from "@/lib/status-colors"
import { registerWorkerJobs } from "@/types/worker-job"

export function registerCryptoLabels(): void {
  registerStatusColors({
    // Crypto invoice
    "Ожидает оплаты": "warning",
    "Частично оплачен": "info",
    "Оплачен": "success",
    "Переплата": "warning",
    "Истёк": "danger",
    // Crypto payment
    "Обнаружен": "warning",
    "Подтверждается": "info",
    "Подписан": "info",
    "Отправлен": "warning",
    "Reorged": "danger",
    // Wallet
    "Свободен": "success",
    "Занят": "warning",
    "Ожидает свипа": "info",
  })

  registerResourceLabels({
    register_crypto_merchant_balance: "Регистр остатков мерчантов",
    register_crypto_balance: "Регистр балансов кошельков",
    report_crypto_turnover: "Отчёт по криптообороту",
  })

  registerWorkerJobs({
    categories: [{ value: "crypto", label: "Криптовалюта" }],
    names: [
      { value: "crypto.expiration", label: "Крипто: истечение инвойсов" },
      { value: "crypto.sweep_eval", label: "Крипто: оценка свипа" },
    ],
  })
}

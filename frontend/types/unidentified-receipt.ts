export type ReceiptJournalState = "open" | "credited" | "orphaned" | "pending"

export const receiptJournalStates: Record<ReceiptJournalState, string> = {
  open: "Не зачислены",
  pending: "Ожидают подтверждения сети",
  credited: "Зачислены",
  orphaned: "Откат блока",
}

export const receiptReasons: Record<string, string> = {
  no_allocation: "Нет счёта на адресе",
  wrong_token: "Не тот токен",
  timestamp_before_lease: "До выдачи адреса",
  overlapping_windows: "Пересечение окон счетов",
  retired_deposit_address: "Выведенный адрес",
  additional_single_payment: "Повторный платёж по счёту",
  dust: "Dust (вероятно спам)",
  treasury_unidentified: "Казначейство",
  matched_receipt: "Сопоставлено",
}

export const receiptRowStates: Record<string, string> = {
  unattributed: "Не зачислено",
  awaiting_approval: "Ждёт подтверждения",
  payment_reorged: "Платёж откачен сетью",
  credited: "Зачислено",
  orphaned: "Откат блока",
  pending: "Ожидает сети",
}

export const receiptRowStateHints: Record<string, string> = {
  payment_reorged: "Платёж счёта сторнирован после отката блока. Вручную зачислить нельзя: при повторном включении перевода платёж распределит поступление снова, а если перевод не вернётся, аудит признает его откатом.",
}

export const receiptAuditStates: Record<string, string> = {
  canonical: "В канонической цепочке",
  orphaned: "Откат блока доказан",
}

export const receiptAttributionTypes: Record<string, string> = {
  CryptoPayment: "Платёж по счёту",
  CryptoMerchantCredit: "Ручное зачисление",
}

export const receiptAttributionStatuses: Record<string, string> = {
  detected: "Обнаружен",
  confirming: "Подтверждается",
  confirmed: "Подтверждён",
  settled: "Зачислен",
  reorged: "Откат блока",
  replaced: "Замещён",
  prepared: "Ждёт подтверждения",
  posted: "Проведено",
  rejected: "Отклонено",
}

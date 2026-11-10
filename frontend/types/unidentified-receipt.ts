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
  credited: "Зачислено",
  orphaned: "Откат блока",
  pending: "Ожидает сети",
}

/**
 * Fee Schedule types — maps to backend FeeSchedule DTO.
 *
 * Used in:
 *   - Global defaults: /api/v1/system/fee-schedule
 *   - Merchant overrides: /api/v1/merchant-admin/merchants/:merchantId/fee-schedule
 */

/** Fee direction — must match backend FeeDirection values. */
export type FeeDirection = "processing" | "withdrawal" | "payout" | "settlement" | "refund"

export const FEE_DIRECTIONS: { value: FeeDirection; label: string; description: string }[] = [
  { value: "processing",  label: "Приём",      description: "Комиссия с входящих платежей клиентов" },
  { value: "withdrawal",  label: "Вывод",       description: "Комиссия за вывод средств мерчантом" },
  { value: "payout",      label: "Выплата",     description: "Комиссия за выплаты клиентам мерчанта" },
  { value: "settlement",  label: "Расчёт",      description: "Комиссия за взаиморасчёт" },
  { value: "refund",      label: "Возврат",     description: "Комиссия за возврат средств" },
]

/** Fee schedule entry from the API. */
export interface FeeScheduleResponse {
  merchantId: string | null
  tokenId: string
  direction: FeeDirection
  fixedFee: string       // crypto minor units as string
  percentBp: number      // basis points [0..10000]
  minFee: string         // crypto minor units
  maxFee: string         // crypto minor units
  updatedAt: string      // ISO 8601
}

/** Request body for upserting a fee schedule entry. */
export interface FeeScheduleUpsertRequest {
  tokenId: string
  direction: FeeDirection
  fixedFee: number       // int64 minor units
  percentBp: number      // basis points [0..10000]
  minFee: number
  maxFee: number
}

/** Request body for deleting a fee schedule entry. */
export interface FeeScheduleDeleteRequest {
  tokenId: string
  direction: FeeDirection
}

/** API list response. */
export interface FeeScheduleListResponse {
  items: FeeScheduleResponse[]
  total: number
}

/**
 * Formats basis points as a human-readable percentage.
 * 100 bp → "1%", 250 bp → "2.5%", 0 bp → "0%"
 */
export function formatBasisPoints(bp: number): string {
  const pct = bp / 100
  return pct % 1 === 0 ? `${pct}%` : `${pct.toFixed(2).replace(/0+$/, "")}%`
}

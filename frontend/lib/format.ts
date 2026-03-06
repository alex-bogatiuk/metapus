/**
 * Shared formatting utilities for the Metapus frontend.
 *
 * Extracted from individual page files to avoid duplication.
 * All formatters use cached Intl instances for performance.
 */

/** Default decimal places for most currencies (RUB, USD, EUR). */
export const DEFAULT_DECIMAL_PLACES = 2

// ⚡ Perf: cached formatters — one per decimalPlaces value, created on demand.
const amountFormatters = new Map<number, Intl.NumberFormat>()
function getAmountFormatter(dp: number): Intl.NumberFormat {
  let fmt = amountFormatters.get(dp)
  if (!fmt) {
    fmt = new Intl.NumberFormat("ru-RU", { minimumFractionDigits: dp, maximumFractionDigits: dp })
    amountFormatters.set(dp, fmt)
  }
  return fmt
}

/** Format MinorUnits to display string with correct decimals (e.g. 1000, 2 → "10,00"). */
export function fmtAmount(minor: number, decimalPlaces = DEFAULT_DECIMAL_PLACES): string {
  const divisor = Math.pow(10, decimalPlaces)
  return getAmountFormatter(decimalPlaces).format(minor / divisor)
}

/** Format ISO date string to dd.mm.yyyy. */
export function fmtDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  })
}

/** Convert backend Quantity (decimal, e.g. 5.0000) to display string. */
export function fromQuantity(q: number): string {
  return q.toString()
}

/** Convert MinorUnits to display string with correct decimals (e.g. 1100, 2 → "11.00"). */
export function fromMinorUnits(m: number, decimalPlaces = DEFAULT_DECIMAL_PLACES): string {
  const divisor = Math.pow(10, decimalPlaces)
  return (m / divisor).toFixed(decimalPlaces)
}

/** Convert display quantity (e.g. "5" or "2.5") to Quantity decimal for backend. */
export function toQuantity(s: string): number {
  return parseFloat(s || "0")
}

/** Convert display price (e.g. "11.50") to MinorUnits int64 (e.g. kopecks, wei). */
export function toMinorUnits(s: string, decimalPlaces = DEFAULT_DECIMAL_PLACES): number {
  const multiplier = Math.pow(10, decimalPlaces)
  return Math.round(parseFloat(s || "0") * multiplier)
}

/** Compute input step for monetary fields based on decimal places (e.g. 2 → "0.01", 9 → "0.000000001"). */
export function moneyStep(decimalPlaces = DEFAULT_DECIMAL_PLACES): string {
  return (1 / Math.pow(10, decimalPlaces)).toFixed(decimalPlaces)
}

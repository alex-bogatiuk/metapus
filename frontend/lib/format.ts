/**
 * Shared formatting utilities for the Metapus frontend.
 *
 * Extracted from individual page files to avoid duplication.
 * All formatters use cached Intl instances for performance.
 */

import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { format as dateFnsFormat } from "date-fns"

/** Default decimal places for most currencies (RUB, USD, EUR). */
export const DEFAULT_DECIMAL_PLACES = 2

// cached formatters — key is dp + format style
const amountFormatters = new Map<string, Intl.NumberFormat>()
function getAmountFormatter(dp: number, formatStyle: string): Intl.NumberFormat {
  const key = `${dp}-${formatStyle}`
  let fmt = amountFormatters.get(key)
  if (!fmt) {
    let locale = "ru-RU"
    let useGrouping = true

    if (formatStyle === "comma") {
      locale = "en-US"
    } else if (formatStyle === "none") {
      useGrouping = false
    }

    fmt = new Intl.NumberFormat(locale, {
      minimumFractionDigits: dp,
      maximumFractionDigits: dp,
      useGrouping
    })
    amountFormatters.set(key, fmt)
  }
  return fmt
}

/** Format MinorUnits to display string with correct decimals dynamically. */
export function fmtAmount(minor: number, decimalPlaces = DEFAULT_DECIMAL_PLACES): string {
  const divisor = Math.pow(10, decimalPlaces)
  const formatStyle = useUserPrefsStore.getState().interface.numberFormat ?? "space"
  return getAmountFormatter(decimalPlaces, formatStyle).format(minor / divisor)
}

/** Format ISO date string dynamically based on user format. */
export function fmtDate(iso: string | Date | undefined | null): string {
  if (!iso) return ""
  const d = typeof iso === "string" ? new Date(iso) : iso
  const dateFormat = useUserPrefsStore.getState().interface.dateFormat ?? "dd.MM.yyyy"
  return dateFnsFormat(d, dateFormat)
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

// ── Portal-specific formatters ────────────────────────────────────────────
// Used by crypto portal pages (invoices, withdrawals, webhooks).
// These operate on string-based minor units (blockchain amounts use BigInt-safe strings).

/**
 * Convert a minor-units string to a human-readable decimal.
 * Example: formatMinorUnits("1500000", 6) → "1.5"
 *
 * Uses pure string arithmetic — no floating point involved.
 * For blockchain amounts where precision matters (unlike fiat fmtAmount which uses Number).
 */
export function formatMinorUnits(minor: string, decimals: number): string {
  if (!minor || decimals === 0) return minor || "0"
  const str = minor.padStart(decimals + 1, "0")
  const intPart = str.slice(0, str.length - decimals)
  const fracPart = str.slice(str.length - decimals)
  return `${intPart}.${fracPart}`.replace(/\.?0+$/, "") || "0"
}

/**
 * Format an ISO-8601 date string for ru-RU locale with time.
 * Example: "2026-05-20T13:00:00Z" → "20.05.2026, 21:00"
 */
export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleDateString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

/**
 * Format an ISO-8601 date string for ru-RU locale without time.
 * Example: "2026-05-20T13:00:00Z" → "20.05.2026"
 */
export function formatDateOnly(iso: string): string {
  return new Date(iso).toLocaleDateString("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  })
}

/** Copy text to clipboard. Silently fails if clipboard API is unavailable. */
export function copyToClipboard(text: string): void {
  navigator.clipboard?.writeText(text)
}

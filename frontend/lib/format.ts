/**
 * Shared formatting utilities for the Metapus frontend.
 *
 * Extracted from individual page files to avoid duplication.
 * All formatters use cached Intl instances for performance.
 */

import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { format as dateFnsFormat } from "date-fns"
import { rawMinorToMajor, formatRawMinor } from "@/lib/money"

/** Exact aggregate/raw-amount display; does not impose int64 on a sum of rows. */
export function fmtRawAmount(minor: string, decimalPlaces: number | null | undefined): string {
  if (decimalPlaces == null) return "—"
  const style = useUserPrefsStore.getState().interface.numberFormat ?? "space"
  return formatRawMinor(minor, decimalPlaces, style)
}

/** Format ISO date string dynamically based on user format. */
export function fmtDate(iso: string | Date | undefined | null): string {
  if (!iso) return ""
  const d = typeof iso === "string" ? new Date(iso) : iso
  const dateFormat = useUserPrefsStore.getState().interface.dateFormat ?? "dd.MM.yyyy"
  return dateFnsFormat(d, dateFormat)
}

/** Convert a human amount to canonical raw units without Number. */
export function toRawUnits(s: string, decimalPlaces: number): string | null {
  const normalized = s.trim().replace(",", ".")
  const match = /^(0|[1-9]\d*)(?:\.(\d*))?$/.exec(normalized)
  if (!match) return null
  const fraction = match[2] ?? ""
  if (fraction.length > decimalPlaces) return null
  const raw = `${match[1]}${fraction.padEnd(decimalPlaces, "0")}`.replace(/^0+(?=\d)/, "")
  return raw || "0"
}

/** Compare canonical non-negative raw-unit strings exactly. */
export function compareRawAmounts(left: string, right: string): number {
  const a = BigInt(left)
  const b = BigInt(right)
  return a < b ? -1 : a > b ? 1 : 0
}

// ── String minor-unit formatters ──────────────────────────────────────────
// These operate on string-based minor units (BigInt-safe, unbounded amounts).

/**
 * Convert a minor-units string to a human-readable decimal.
 * Example: formatMinorUnits("1500000", 6) → "1.5"
 *
 * Uses pure string arithmetic — no floating point involved.
 * Works for both positive and negative balances.
 */
export function formatMinorUnits(minor: string, decimals: number): string {
  const exact = rawMinorToMajor(minor || "0", decimals)
  return decimals === 0 ? exact : exact.replace(/\.?0+$/, "")
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

/** Copy text to clipboard. Silently fails if clipboard API is unavailable. */
export function copyToClipboard(text: string): void {
  navigator.clipboard?.writeText(text)
}

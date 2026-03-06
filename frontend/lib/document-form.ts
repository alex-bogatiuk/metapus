import { api } from "@/lib/api"

// ── Shared line state for document forms ────────────────────────────────

export interface FormLine {
  _key: number
  productId: string
  productName: string
  unitId: string
  unitName: string
  quantity: string
  unitPrice: string
  vatRateId: string
  vatRateName: string
  vatPercent: string
  discountPercent: string
  // read-only display from response (edit mode)
  amount?: number
  vatAmount?: number
}

export function emptyLine(key: number): FormLine {
  return { _key: key, productId: "", productName: "", unitId: "", unitName: "", quantity: "", unitPrice: "", vatRateId: "", vatRateName: "", vatPercent: "20", discountPercent: "0" }
}

// ── VAT rate helpers ────────────────────────────────────────────────────

/** Fetch VAT rate percentage from backend by ID. Returns integer percent (e.g. 20). */
export async function fetchVatRatePercent(id: string): Promise<string> {
  try {
    const data = await api.vatRates.get(id)
    return String(Math.round(parseFloat(data.rate ?? "0")))
  } catch {
    return "0"
  }
}

// ── Line amount calculation ─────────────────────────────────────────────

/** Locally calculate line amount & VAT from editable fields (MinorUnits). */
export function calcLineAmounts(line: FormLine, includesVat: boolean, dp: number): { amount: number; vatAmount: number } {
  const qty = parseFloat(line.quantity || "0")
  const price = parseFloat(line.unitPrice || "0")
  const multiplier = Math.pow(10, dp)
  const lineAmount = Math.round(qty * price * multiplier)
  const vatPct = parseInt(line.vatPercent || "0")
  const vat = includesVat
    ? Math.round(lineAmount - lineAmount / (1 + vatPct / 100))
    : Math.round(lineAmount * vatPct / 100)
  return { amount: lineAmount, vatAmount: vat }
}

// ── Totals computation ──────────────────────────────────────────────────

export function computeTotals(
  lines: FormLine[],
  amountIncludesVat: boolean,
  decimalPlaces: number,
): { totalAmount: number; totalVat: number } {
  let totalAmount = 0
  let totalVat = 0
  for (const l of lines) {
    const qty = parseFloat(l.quantity || "0")
    const price = parseFloat(l.unitPrice || "0")
    const multiplier = Math.pow(10, decimalPlaces)
    const lineAmount = qty * price * multiplier
    const vatPct = parseInt(l.vatPercent || "0")
    const vat = amountIncludesVat
      ? lineAmount - lineAmount / (1 + vatPct / 100)
      : lineAmount * vatPct / 100
    totalAmount += lineAmount
    totalVat += vat
  }
  return { totalAmount: Math.round(totalAmount), totalVat: Math.round(totalVat) }
}

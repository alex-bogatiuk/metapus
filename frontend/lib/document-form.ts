import { fromQuantity, fromMinorUnits, DEFAULT_DECIMAL_PLACES } from "@/lib/format"
import { api } from "@/lib/api"
import type { RefDisplay } from "@/types/document"

// ── Shared line state for document forms ────────────────────────────────

export interface FormLine {
  _key: number
  nomenclatureId: string
  nomenclatureName: string
  nomenclatureCode: string
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
  return { _key: key, nomenclatureId: "", nomenclatureName: "", nomenclatureCode: "", unitId: "", unitName: "", quantity: "", unitPrice: "", vatRateId: "", vatRateName: "", vatPercent: "20", discountPercent: "0" }
}

// ── Generic document line response → FormLine mapping ───────────────────

/**
 * Common shape for document line responses (GoodsReceiptLineResponse,
 * GoodsIssueLineResponse, etc.). Any line response conforming to this
 * shape can be mapped via `mapLinesToFormLines`.
 */
export interface DocumentLineResponseLike {
  nomenclatureId: string
  unitId: string
  quantity: number
  unitPrice: number
  vatRateId: string
  vatPercent: number
  discountPercent: string
  amount: number
  vatAmount: number
  nomenclature?: RefDisplay
  unit?: RefDisplay
  vatRate?: RefDisplay
}

/**
 * Map server line responses → FormLine[].
 *
 * Used in:
 * - `mapDocToState()` on `[id]` pages (preserveAmounts = true)
 * - `copyFrom` on `new` pages (preserveAmounts = false, default)
 *
 * @param lines         — raw line responses from the server
 * @param decimalPlaces — currency scale for unitPrice conversion
 * @param options       — preserveAmounts: keep server-computed amount/vatAmount
 *                        startKey: starting _key (defaults to 1)
 */
export function mapLinesToFormLines(
  lines: DocumentLineResponseLike[],
  decimalPlaces: number = DEFAULT_DECIMAL_PLACES,
  options?: { preserveAmounts?: boolean; startKey?: number },
): { mapped: FormLine[]; nextKey: number } {
  const startKey = options?.startKey ?? 1
  const preserveAmounts = options?.preserveAmounts ?? false

  const mapped = lines.map((l, i): FormLine => ({
    _key: startKey + i,
    nomenclatureId: l.nomenclatureId,
    nomenclatureName: l.nomenclature?.name || "",
    nomenclatureCode: l.nomenclature?.code || "",
    unitId: l.unitId,
    unitName: l.unit?.name || "",
    quantity: fromQuantity(l.quantity),
    unitPrice: fromMinorUnits(l.unitPrice, decimalPlaces),
    vatRateId: l.vatRateId,
    vatRateName: l.vatRate?.name || "",
    vatPercent: String(l.vatPercent ?? 0),
    discountPercent: l.discountPercent || "0",
    ...(preserveAmounts ? { amount: l.amount, vatAmount: l.vatAmount } : {}),
  }))

  return { mapped, nextKey: startKey + mapped.length }
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

  if (includesVat) {
    // Price includes VAT → extract VAT from gross amount
    const vat = Math.round(lineAmount - lineAmount / (1 + vatPct / 100))
    return { amount: lineAmount, vatAmount: vat }
  }
  // Price excludes VAT → add VAT on top (matches backend: Amount = netAmount + vatAmount)
  const vat = Math.round(lineAmount * vatPct / 100)
  return { amount: lineAmount + vat, vatAmount: vat }
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
    const { amount, vatAmount } = calcLineAmounts(l, amountIncludesVat, decimalPlaces)
    totalAmount += amount
    totalVat += vatAmount
  }
  return { totalAmount, totalVat }
}

// ── Picker ↔ FormLine integration ───────────────────────────────────────

import type { PickedItem, ExistingPickerLine } from "@/types/picker"

/**
 * Convert current form lines to ExistingPickerLine[] for pre-populating the picker.
 * Aggregates quantities per nomenclatureId (in case the same product appears on multiple lines).
 */
export function linesToExistingPickerLines(lines: FormLine[]): ExistingPickerLine[] {
  const map = new Map<string, ExistingPickerLine>()
  for (const l of lines) {
    if (!l.nomenclatureId) continue
    const qty = parseFloat(l.quantity || "0")
    if (qty <= 0) continue
    const existing = map.get(l.nomenclatureId)
    if (existing) {
      existing.quantity += qty
    } else {
      map.set(l.nomenclatureId, {
        nomenclatureId: l.nomenclatureId,
        nomenclatureName: l.nomenclatureName,
        nomenclatureCode: l.nomenclatureCode || undefined,
        unitId: l.unitId || undefined,
        unitName: l.unitName || undefined,
        quantity: qty,
      })
    }
  }
  return Array.from(map.values())
}

/**
 * Merge picker results into existing form lines.
 *
 * Logic:
 *   - If an existing line's nomenclatureId is in pickedItems → update its quantity
 *   - If a pickedItem is NOT in existing lines → add a new line
 *   - If an existing line's nomenclatureId is in knownProductIds but NOT in pickedItems
 *     → user removed it in the picker → delete the line
 *   - Lines whose nomenclatureId was NOT in the picker at all → keep untouched
 *
 * @param knownProductIds — IDs of products that were pre-loaded into the picker
 *   (from existingLines → linesToExistingPickerLines). Enables distinguishing
 *   "removed in picker" from "not touched by picker".
 *
 * Returns { lines, nextKey } for spreading into form state.
 */
export function mergePickedIntoLines(
  existingLines: FormLine[],
  pickedItems: PickedItem[],
  nextKey: number,
  knownProductIds?: Set<string>,
): { lines: FormLine[]; nextKey: number } {
  const pickedMap = new Map<string, PickedItem>()
  for (const item of pickedItems) {
    pickedMap.set(item.id, item)
  }

  // Track which picked items were matched to existing lines
  const matchedIds = new Set<string>()
  let key = nextKey

  // Update existing lines
  const updatedLines: FormLine[] = []
  for (const line of existingLines) {
    const picked = line.nomenclatureId ? pickedMap.get(line.nomenclatureId) : undefined
    if (picked) {
      // Product exists in picker results → update quantity
      matchedIds.add(picked.id)
      updatedLines.push({
        ...line,
        quantity: String(picked.quantity),
        // Reset computed amounts so they get recalculated
        amount: undefined,
        vatAmount: undefined,
      })
    } else if (knownProductIds && line.nomenclatureId && knownProductIds.has(line.nomenclatureId)) {
      // Product was in the picker but removed (qty set to 0) → skip (delete line)
    } else {
      // Product not touched by picker → keep line as-is
      updatedLines.push(line)
    }
  }

  // Add new lines for unmatched picker items
  for (const item of pickedItems) {
    if (matchedIds.has(item.id)) continue
    updatedLines.push({
      ...emptyLine(key++),
      nomenclatureId: item.id,
      nomenclatureName: item.name,
      unitId: item.unitId ?? "",
      unitName: item.unitName ?? "",
      quantity: String(item.quantity),
    })
  }

  return { lines: updatedLines, nextKey: key }
}

// ── Table part export helpers ───────────────────────────────────────────

import type { TablePartExportColumn } from "@/hooks/useTablePartExport"

/** Standard export column presets for goods-type lines (Товары). */
export const GOODS_LINE_EXPORT_COLUMNS = {
  /** Columns with computed amounts (edit pages with server-computed data) */
  withAmounts: [
    { key: "rowNumber", label: "N" },
    { key: "nomenclatureName", label: "Товар" },
    { key: "unitName", label: "Ед. изм." },
    { key: "quantity", label: "Кол-во" },
    { key: "unitPrice", label: "Цена" },
    { key: "amount", label: "Сумма" },
    { key: "vatAmount", label: "НДС" },
    { key: "vatRateName", label: "Ставка НДС" },
    { key: "discountPercent", label: "Скидка %" },
  ] satisfies TablePartExportColumn[],
  /** Columns without amounts (new documents, no server data yet) */
  withoutAmounts: [
    { key: "rowNumber", label: "N" },
    { key: "nomenclatureName", label: "Товар" },
    { key: "unitName", label: "Ед. изм." },
    { key: "quantity", label: "Кол-во" },
    { key: "unitPrice", label: "Цена" },
    { key: "vatRateName", label: "Ставка НДС" },
    { key: "discountPercent", label: "Скидка %" },
  ] satisfies TablePartExportColumn[],
} as const

/**
 * Map FormLine[] → export-ready flat rows for XLSX rendering.
 *
 * Used by `useDocumentLinesExport` hook — centralizes all FormLine → export
 * transformation logic so document pages have zero duplication.
 *
 * @param lines              — form state lines
 * @param decimalPlaces      — currency scale for amount conversion
 * @param amountIncludesVat  — whether prices include VAT
 * @param options.includeAmounts — include amount/vatAmount (edit pages)
 */
export function buildLinesExportRows(
  lines: FormLine[],
  decimalPlaces: number,
  amountIncludesVat: boolean,
  options?: { includeAmounts?: boolean },
): Record<string, unknown>[] {
  const includeAmounts = options?.includeAmounts ?? false
  const divisor = Math.pow(10, decimalPlaces)

  return lines.map((l, i) => {
    const base: Record<string, unknown> = {
      rowNumber: i + 1,
      nomenclatureName: l.nomenclatureName,
      unitName: l.unitName,
      quantity: parseFloat(l.quantity || "0"),
      unitPrice: parseFloat(l.unitPrice || "0"),
      vatRateName: l.vatRateName,
      discountPercent: l.discountPercent || "0",
    }

    if (includeAmounts) {
      const computed = calcLineAmounts(l, amountIncludesVat, decimalPlaces)
      base.amount = (l.amount ?? computed.amount) / divisor
      base.vatAmount = (l.vatAmount ?? computed.vatAmount) / divisor
    }

    return base
  })
}

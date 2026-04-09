"use client"

import React, { useCallback } from "react"
import { Trash2 } from "lucide-react"
import { ReferenceField } from "@/components/shared/reference-field"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { fmtAmount, moneyStep } from "@/lib/format"
import { type FormLine, fetchVatRatePercent, calcLineAmounts } from "@/lib/document-form"

// ── Props ────────────────────────────────────────────────────────────────

export interface DocumentLineRowProps {
  line: FormLine
  /** 1-based row number displayed in the first column */
  rowNumber: number
  /** Number of decimal places for amount display (from currency scale) */
  decimalPlaces: number
  /** Whether amounts include VAT (affects line totals calculation) */
  amountIncludesVat: boolean

  /** Update a single field on this line */
  onUpdateField: (key: number, field: keyof FormLine, value: string) => void
  /** Update a reference field (id + name) on this line */
  onUpdateRef: (key: number, patch: Partial<FormLine>) => void
  /** Update a reference field and then async-resolve VAT percent */
  onUpdateVatRate: (key: number, id: string, name: string) => void
  /** Remove this line */
  onRemove: (key: number) => void

  /**
   * If true, shows amount + VAT columns (edit mode with server-computed values).
   * When false, those columns are hidden (create mode has no server values).
   */
  showAmounts?: boolean
}

// ── Component (memoised) ─────────────────────────────────────────────────

export const DocumentLineRow = React.memo(function DocumentLineRow({
  line,
  rowNumber,
  decimalPlaces,
  amountIncludesVat,
  onUpdateField,
  onUpdateRef,
  onUpdateVatRate,
  onRemove,
  showAmounts = false,
}: DocumentLineRowProps) {
  // ── Stable callbacks that delegate to parent via _key ────────────────
  const handleProductChange = useCallback(
    (id: string, name: string) =>
      onUpdateRef(line._key, { productId: id, productName: name }),
    [line._key, onUpdateRef],
  )

  const handleUnitChange = useCallback(
    (id: string, name: string) =>
      onUpdateRef(line._key, { unitId: id, unitName: name }),
    [line._key, onUpdateRef],
  )

  const handleVatRateChange = useCallback(
    (id: string, name: string) => onUpdateVatRate(line._key, id, name),
    [line._key, onUpdateVatRate],
  )

  const handleQuantityChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) =>
      onUpdateField(line._key, "quantity", e.target.value),
    [line._key, onUpdateField],
  )

  const handlePriceChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) =>
      onUpdateField(line._key, "unitPrice", e.target.value),
    [line._key, onUpdateField],
  )

  const handleVatPercentChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) =>
      onUpdateField(line._key, "vatPercent", e.target.value),
    [line._key, onUpdateField],
  )

  const handleRemove = useCallback(
    () => onRemove(line._key),
    [line._key, onRemove],
  )

  // ── Computed amounts (only when showAmounts=true) ────────────────────
  const displayAmount = showAmounts
    ? line.amount !== undefined
      ? fmtAmount(line.amount, decimalPlaces)
      : fmtAmount(calcLineAmounts(line, amountIncludesVat, decimalPlaces).amount, decimalPlaces)
    : null

  const displayVat = showAmounts
    ? line.vatAmount !== undefined
      ? fmtAmount(line.vatAmount, decimalPlaces)
      : fmtAmount(calcLineAmounts(line, amountIncludesVat, decimalPlaces).vatAmount, decimalPlaces)
    : null

  return (
    <tr className="group border-b hover:bg-primary/5 transition-colors">
      <td className="px-2 py-1.5 text-center text-xs text-muted-foreground">{rowNumber}</td>
      <td className="px-1 py-1">
        <ReferenceField
          compact
          value={line.productId}
          displayName={line.productName}
          apiEndpoint="/catalog/nomenclature"
          placeholder="Номенклатура"
          onChange={handleProductChange}
        />
      </td>
      <td className="px-1 py-1">
        <ReferenceField
          compact
          value={line.unitId}
          displayName={line.unitName}
          apiEndpoint="/catalog/units"
          placeholder="Ед. изм."
          onChange={handleUnitChange}
        />
      </td>
      <td className="px-1 py-1">
        <Input
          className="h-7 text-right font-mono text-xs"
          type="number"
          step="0.001"
          value={line.quantity}
          onChange={handleQuantityChange}
        />
      </td>
      <td className="px-1 py-1">
        <Input
          className="h-7 text-right font-mono text-xs"
          type="number"
          step={moneyStep(decimalPlaces)}
          value={line.unitPrice}
          onChange={handlePriceChange}
        />
      </td>
      {showAmounts && (
        <>
          <td className="px-1 py-1 text-right font-mono text-xs text-muted-foreground">
            {displayAmount}
          </td>
          <td className="px-1 py-1 text-right font-mono text-xs text-muted-foreground">
            {displayVat}
          </td>
        </>
      )}
      <td className="px-1 py-1">
        <ReferenceField
          compact
          value={line.vatRateId}
          displayName={line.vatRateName}
          apiEndpoint="/catalog/vat-rates"
          placeholder="Ставка НДС"
          onChange={handleVatRateChange}
        />
      </td>
      {!showAmounts && (
        <td className="px-1 py-1">
          <Input
            className="h-7 text-right font-mono text-xs"
            type="number"
            value={line.vatPercent}
            onChange={handleVatPercentChange}
          />
        </td>
      )}
      <td className="px-1 py-1">
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
          onClick={handleRemove}
        >
          <Trash2 className="h-4 w-4 text-destructive/70" />
        </Button>
      </td>
    </tr>
  )
})

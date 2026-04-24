"use client"

import React, { useCallback } from "react"
import { Trash2 } from "lucide-react"
import { ReferenceField } from "@/components/shared/reference-field"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { useCompactMode } from "@/hooks/useCompactMode"
import { fmtAmount, moneyStep } from "@/lib/format"
import { type FormLine, calcLineAmounts } from "@/lib/document-form"

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

  // ── Drag-and-drop (optional) ─────────────────────────────────────────
  /** Ref from useSortable — attach to the <tr> element */
  dragRef?: (node: HTMLElement | null) => void
  /** Style from useSortable — transform/transition for drag animation */
  dragStyle?: React.CSSProperties
  /** Drag handle slot — rendered inside the N cell, appears on group hover */
  dragHandleSlot?: React.ReactNode
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
  dragRef,
  dragStyle,
  dragHandleSlot,
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

  const compact = useCompactMode()
  const cellPy = compact ? "py-0.5" : "py-1"
  const inputH = compact ? "h-6" : "h-7"
  const btnSize = compact ? "h-6 w-6" : "h-7 w-7"

  return (
    <tr ref={dragRef} style={dragStyle} className="group border-b hover:bg-primary/5 transition-colors">
      <td className={cn("px-2 text-center text-xs text-muted-foreground", compact ? "py-1" : "py-1.5")}>
        <span className="inline-flex items-center gap-0.5">
          {dragHandleSlot}
          {rowNumber}
        </span>
      </td>
      <td className={cn("px-1", cellPy)}>
        <ReferenceField
          compact
          value={line.productId}
          displayName={line.productName}
          apiEndpoint="/catalog/nomenclature"
          placeholder="Номенклатура"
          onChange={handleProductChange}
        />
      </td>
      <td className={cn("px-1", cellPy)}>
        <ReferenceField
          compact
          value={line.unitId}
          displayName={line.unitName}
          apiEndpoint="/catalog/units"
          placeholder="Ед. изм."
          onChange={handleUnitChange}
        />
      </td>
      <td className={cn("px-1", cellPy)}>
        <Input
          className={cn(inputH, "text-right font-mono text-xs")}
          type="number"
          step="0.001"
          value={line.quantity}
          onChange={handleQuantityChange}
        />
      </td>
      <td className={cn("px-1", cellPy)}>
        <Input
          className={cn(inputH, "text-right font-mono text-xs")}
          type="number"
          step={moneyStep(decimalPlaces)}
          value={line.unitPrice}
          onChange={handlePriceChange}
        />
      </td>
      {showAmounts && (
        <>
          <td className={cn("px-1 text-right font-mono text-xs text-muted-foreground", cellPy)}>
            {displayAmount}
          </td>
          <td className={cn("px-1 text-right font-mono text-xs text-muted-foreground", cellPy)}>
            {displayVat}
          </td>
        </>
      )}
      <td className={cn("px-1", cellPy)}>
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
        <td className={cn("px-1", cellPy)}>
          <Input
            className={cn(inputH, "text-right font-mono text-xs")}
            type="number"
            value={line.vatPercent}
            onChange={handleVatPercentChange}
          />
        </td>
      )}
      <td className={cn("px-1", cellPy)}>
        <Button
          variant="ghost"
          size="icon"
          className={cn(btnSize, "opacity-0 group-hover:opacity-100 transition-opacity")}
          onClick={handleRemove}
        >
          <Trash2 className="h-4 w-4 text-destructive/70" />
        </Button>
      </td>
    </tr>
  )
})

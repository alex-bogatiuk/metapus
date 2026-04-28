"use client"

import React, { useCallback, useRef } from "react"
import { Trash2 } from "lucide-react"
import { ReferenceField } from "@/components/shared/reference-field"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { useCompactMode } from "@/hooks/useCompactMode"
import { fmtAmount, moneyStep } from "@/lib/format"
import { type FormLine, calcLineAmounts } from "@/lib/document-form"
import { advanceToNextField, activateFirstField, advanceToFirstEmptyField } from "@/lib/table-field-navigation"

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
  /**
   * Cascading product selection: saves product, async-fetches nomenclature,
   * cascade-fills unit + vatRate. Returns Promise so caller can smart-advance.
   */
  onProductSelect: (key: number, id: string, name: string) => Promise<void>
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

  // ── Tab traversal (M8 + M9) ──────────────────────────────────────────
  /** Whether this is the last row in the table (triggers auto-create on Tab) */
  isLastRow?: boolean
  /** Called when user presses Tab from the last input of the last row */
  onTabToNextRow?: () => void
}

// ── Component (memoised) ─────────────────────────────────────────────────

export const DocumentLineRow = React.memo(function DocumentLineRow({
  line,
  rowNumber,
  decimalPlaces,
  amountIncludesVat,
  onUpdateField,
  onUpdateRef,
  onProductSelect,
  onUpdateVatRate,
  onRemove,
  showAmounts = false,
  dragRef,
  dragStyle,
  dragHandleSlot,
  isLastRow,
  onTabToNextRow,
}: DocumentLineRowProps) {
  const rowRef = useRef<HTMLTableRowElement>(null)

  // ── Stable callbacks that delegate to parent via _key ────────────────
  // Product selection: cascade-fill from nomenclature, then smart-advance
  const handleProductChange = useCallback(
    async (id: string, name: string) => {
      await onProductSelect(line._key, id, name)
      // After cascade completes + React re-renders, advance to first empty field
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          if (rowRef.current) {
            advanceToFirstEmptyField(rowRef.current)
          }
        })
      })
    },
    [line._key, onProductSelect],
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

  // ── Tab traversal ─────────────────────────────────────────────────────
  // Handles Tab from plain <input> fields (quantity, price, vat%).
  // If the next editable field is a combobox → click it (opens dropdown).
  // If it's another input → let browser Tab handle it naturally.
  // At end of row → advance to first field of next row (or create new row).
  const handleTabTraversal = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key !== "Tab" || e.shiftKey) return
      const target = e.target as HTMLElement
      const td = target.closest("td")
      if (!td) return

      // Check if the next field in the row is a combobox
      let nextTd = td.nextElementSibling as HTMLElement | null
      while (nextTd) {
        const combobox = nextTd.querySelector<HTMLElement>("[role=combobox]")
        if (combobox) {
          // Next field is a combobox — click to open dropdown
          e.preventDefault()
          combobox.click()
          return
        }
        const input = nextTd.querySelector<HTMLInputElement>("input:not([type=hidden])")
        if (input) {
          // Next field is a plain input — let browser default Tab work
          return
        }
        // Cell has no editable field (e.g. read-only amount) — skip it
        nextTd = nextTd.nextElementSibling as HTMLElement | null
      }

      // No more fields in this row — move to next row
      e.preventDefault()
      const tr = target.closest("tr")
      if (!tr) return

      if (isLastRow && onTabToNextRow) {
        onTabToNextRow()
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            activateFirstField(tr.nextElementSibling)
          })
        })
      } else {
        activateFirstField(tr.nextElementSibling)
      }
    },
    [isLastRow, onTabToNextRow],
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
    <tr ref={(node) => { rowRef.current = node; dragRef?.(node) }} style={dragStyle} className="group border-b hover:bg-primary/5 transition-colors animate-row-in">
      <td className={cn("px-2 text-center text-xs text-muted-foreground", compact ? "py-1" : "py-1.5")}>
        <span className="inline-flex items-center gap-0.5">
          {dragHandleSlot}
          {rowNumber}
        </span>
      </td>
      <td className={cn("px-1", cellPy)}>
        <ReferenceField
          compact
          value={line.nomenclatureId}
          displayName={line.nomenclatureName}
          apiEndpoint="/catalog/nomenclatures"
          placeholder="Номенклатура"
          onChange={handleProductChange}
        />
      </td>
      <td className={cn("px-1", cellPy)}>
        <ReferenceField
          compact
          autoAdvance
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
          onKeyDown={handleTabTraversal}
        />
      </td>
      <td className={cn("px-1", cellPy)}>
        <Input
          className={cn(inputH, "text-right font-mono text-xs")}
          type="number"
          step={moneyStep(decimalPlaces)}
          value={line.unitPrice}
          onChange={handlePriceChange}
          onKeyDown={handleTabTraversal}
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
          autoAdvance
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
            onKeyDown={handleTabTraversal}
          />
        </td>
      )}
      <td className={cn("px-1", cellPy)}>
        <Button
          variant="ghost"
          size="icon"
          tabIndex={-1}
          className={cn(btnSize, "opacity-0 group-hover:opacity-100 transition-opacity")}
          onClick={handleRemove}
        >
          <Trash2 className="h-4 w-4 text-destructive/70" />
        </Button>
      </td>
    </tr>
  )
})

import { z } from "zod"
import { minorUnitsSchema, formatRawMinor } from "@/lib/money"
import { moneyTotals, formatReportTotal } from "@/lib/report-money"
import type { DocumentMovement, MovementColumnDef } from "@/types/common"

const unitSchema = z.object({ id: z.string().min(1), currencyCode: z.string().min(1), decimalPlaces: z.int().min(0).max(18) })

export function movementMoney(row: DocumentMovement, column: MovementColumnDef) {
  const raw = row.data[column.key]
  if (raw == null) return null
  if (!column.currencyRef) throw new Error("Не задана валюта движения")
  const unit = unitSchema.parse(row.data[column.currencyRef])
  return { amount: minorUnitsSchema.parse(raw), currencyId: unit.id, currencyCode: unit.currencyCode, decimalPlaces: unit.decimalPlaces }
}

export function formatMovementMoney(row: DocumentMovement, column: MovementColumnDef) {
  const value = movementMoney(row, column)
  return value ? `${formatRawMinor(value.amount, value.decimalPlaces)} ${value.currencyCode}` : "—"
}

export function movementMoneyTotal(rows: DocumentMovement[], column: MovementColumnDef, func: "sum" | "avg" | "count") {
  const values = rows.flatMap(row => { const value = movementMoney(row, column); return value ? [value] : [] })
  const result = moneyTotals(values, { column: "amount", func, money: { currencyId: "currencyId", currencyCode: "currencyCode", decimalPlaces: "decimalPlaces" } })
  return formatReportTotal(result)
}

export function validateMovementMoney(rows: DocumentMovement[]) {
  for (const row of rows) for (const column of row.columns) if (column.type === "amount") movementMoney(row, column)
}

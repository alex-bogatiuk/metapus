import { z } from "zod"
import { minorUnitsSchema, cryptoUnitsSchema, formatRawMinor } from "@/lib/money"
import type { MoneySources, MoneyTotal, ReportColumnDef, ReportTotalDef, ReportTotalValue } from "@/types/report-meta"

export function readReportMoney(row: object, column: string, sources: MoneySources) {
  const amounts = z.object({ [column]: (sources.amountType === "raw" ? cryptoUnitsSchema : minorUnitsSchema).nullish() }).parse(row)
  const amount = amounts[column]
  if (amount == null) return null
  const strings = z.object({ [sources.currencyId]: z.string().min(1), [sources.currencyCode]: z.string().min(1) }).parse(row)
  const scales = z.object({ [sources.decimalPlaces]: z.int().min(0).max(18) }).parse(row)
  return { amount, currencyId: strings[sources.currencyId], currencyCode: strings[sources.currencyCode], decimalPlaces: scales[sources.decimalPlaces] }
}

/** Called at the HTTP boundary before monetary report rows enter the store. */
export function validateReportMoney(items: object[], columns: ReportColumnDef[]): void {
  for (const column of columns) {
    if (column.type !== "money") continue
    if (!column.money) throw new Error("Не задана валюта денежной колонки")
    for (const row of items) readReportMoney(row, column.key, column.money)
  }
}

export function formatReportMoney(row: object, column: ReportColumnDef): string {
  if (!column.money) throw new Error("Не задана валюта денежной колонки")
  const value = readReportMoney(row, column.key, column.money)
  return value ? `${formatRawMinor(value.amount, value.decimalPlaces)} ${value.currencyCode}` : "—"
}

export function moneyTotals(items: object[], definition: ReportTotalDef): MoneyTotal[] | number {
  if (!definition.money) throw new Error("Не задана валюта итога")
  const groups = new Map<string, { unit: Omit<MoneyTotal, "amount">; values: string[] }>()
  let count = 0
  for (const item of items) {
    const value = readReportMoney(item, definition.column, definition.money)
    if (!value) continue
    count++
    let group = groups.get(value.currencyId)
    if (!group) {
      group = { unit: { currencyId: value.currencyId, currencyCode: value.currencyCode, decimalPlaces: value.decimalPlaces }, values: [] }
      groups.set(value.currencyId, group)
    }
    if (group.unit.currencyCode !== value.currencyCode || group.unit.decimalPlaces !== value.decimalPlaces) throw new Error("Несогласованная точность валюты")
    group.values.push(value.amount)
  }
  if (definition.func === "count") return count
  return [...groups.values()].map(({ unit, values }) => {
    const numbers = values.map(value => BigInt(value))
    let result = numbers.reduce((sum, value) => sum + value, 0n)
    if (definition.func === "avg") {
      const count = BigInt(numbers.length)
      const sign = result < 0n ? -1n : 1n
      const absolute = result * sign
      result = sign * (absolute / count + (absolute % count * 2n >= count ? 1n : 0n))
    }
    if (definition.func === "min") result = numbers.reduce((a, b) => a < b ? a : b)
    if (definition.func === "max") result = numbers.reduce((a, b) => a > b ? a : b)
    return { ...unit, amount: result.toString() }
  })
}

export function formatReportTotal(value: ReportTotalValue): string {
  if (typeof value === "number") return value.toLocaleString("ru-RU", { maximumFractionDigits: 4 })
  return value.map(total => `${formatRawMinor(total.amount, total.decimalPlaces)} ${total.currencyCode}`).join("; ")
}

import { z } from "zod"
import Decimal from "decimal.js"

const MIN_INT64 = -9223372036854775808n
const MAX_INT64 = 9223372036854775807n
const canonicalInteger = /^(?:0|-[1-9]\d*|[1-9]\d*)$/

/** HTTP amounts are canonical, checked int64 strings, never JavaScript numbers. */
export const minorUnitsSchema = z.string().max(20).regex(canonicalInteger).refine(
  value => canonicalInteger.test(value) && value.length <= 20 && BigInt(value) >= MIN_INT64 && BigInt(value) <= MAX_INT64,
  "Сумма выходит за допустимый диапазон",
).brand<"MinorUnits">()
export type MinorUnits = z.infer<typeof minorUnitsSchema>
/** Separate contract for non-negative raw asset amounts (NUMERIC, not BIGINT). */
export const rawUnitsSchema = z.string().regex(/^(?:0|[1-9]\d*)$/).brand<"RawUnits">()
export type RawUnits = z.infer<typeof rawUnitsSchema>
export const ZERO_MINOR_UNITS = minorUnitsSchema.parse("0")

export function minorUnits(value: string | bigint): MinorUnits {
  const result = minorUnitsSchema.safeParse(typeof value === "bigint" ? value.toString() : value)
  if (!result.success) throw new Error("Сумма должна быть целым числом минимальных единиц в диапазоне int64")
  return result.data
}

export function checkCurrencyScale(scale: number): void {
  if (!Number.isInteger(scale) || scale < 0 || scale > 18) throw new Error("Недопустимая точность валюты")
}

export function requireCurrencyScale(scale: number | null | undefined): number {
  if (scale == null) throw new Error("Точность валюты недоступна. Расчёт и сохранение сумм невозможны")
  checkCurrencyScale(scale)
  return scale
}

/** Exact conversion for user input. Extra fractional digits are rejected, never rounded. */
export function majorToMinor(text: string, scale: number): MinorUnits {
  checkCurrencyScale(scale)
  const normalized = text.trim().replace(",", ".")
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(normalized)
  if (!match || normalized.length > 64) throw new Error("Введите сумму в десятичной записи")
  const fraction = match[3] ?? ""
  if (fraction.length > scale) throw new Error(`Допустимо знаков после запятой: ${scale}`)
  const raw = BigInt(match[2] + fraction.padEnd(scale, "0"))
  return minorUnits(match[1] ? -raw : raw)
}

/** Decimal string for forms and exports. Integer arithmetic also supports MinInt64. */
export function minorToMajor(value: MinorUnits, scale: number): string {
  return rawMinorToMajor(value, scale)
}

/** Unbounded raw integer variant for raw asset amounts and sums of multiple valid int64 values. */
export function rawMinorToMajor(value: string, scale: number): string {
  checkCurrencyScale(scale)
  if (!canonicalInteger.test(value)) throw new Error("Некорректная сумма")
  const negative = value.startsWith("-")
  const digits = (negative ? value.slice(1) : value).padStart(scale + 1, "0")
  const major = scale === 0 ? digits : `${digits.slice(0, -scale)}.${digits.slice(-scale)}`
  return negative ? `-${major}` : major
}

export function formatRawMinor(value: string, scale: number, style: string = "space"): string {
  const [integer, fraction] = rawMinorToMajor(value, scale).split(".")
  const sign = integer.startsWith("-") ? "-" : ""
  const digits = sign ? integer.slice(1) : integer
  const group = style === "comma" ? "," : "\u00a0"
  const grouped = style === "none" ? digits : digits.replace(/\B(?=(\d{3})+(?!\d))/g, group)
  return `${sign}${grouped}${fraction === undefined ? "" : `${style === "comma" ? "." : ","}${fraction}`}`
}

// Independent precision/rounding, unaffected by other widgets' Decimal settings.
export const MoneyDecimal = Decimal.clone({ precision: 80, rounding: Decimal.ROUND_HALF_UP })

export interface LineMoneyInput {
  quantity: string
  unitPrice: string
  discountPercent: string
  vatPercent: string
}

/** Mirrors backend net/VAT rounding; never changes document posting rules. */
export function calculateLineMoney(input: LineMoneyInput, includesVat: boolean, scale: number): { amount: MinorUnits; vatAmount: MinorUnits } {
  const quantity = new MoneyDecimal(majorToMinor(input.quantity || "0", 4)).div(10000)
  const price = new MoneyDecimal(majorToMinor(input.unitPrice || "0", scale))
  const percent = (text: string) => {
    if (!/^\d+(?:\.\d+)?$/.test(text) || text.length > 32) throw new Error("Некорректный процент")
    const value = new MoneyDecimal(text)
    if (value.lt(0) || value.gt(100)) throw new Error("Процент должен быть от 0 до 100")
    return value
  }
  const discount = percent(input.discountPercent || "0")
  const vat = percent(input.vatPercent || "0")
  if (!vat.isInteger()) throw new Error("Ставка НДС должна быть целой")
  // shopspring/decimal.Div uses 16 decimal places in the existing backend.
  const divide = (value: Decimal, divisor: Decimal.Value) => value.div(divisor).toDecimalPlaces(16, Decimal.ROUND_HALF_UP)
  const base = quantity.mul(price)
  const net = base.minus(divide(base.mul(discount), 100))
  const vatAmount = minorUnits(divide(net.mul(vat), includesVat ? vat.plus(100) : 100).toFixed(0, Decimal.ROUND_HALF_UP))
  const netAmount = minorUnits(net.toFixed(0, Decimal.ROUND_HALF_UP))
  return {
    amount: includesVat ? netAmount : minorUnits(BigInt(netAmount) + BigInt(vatAmount)),
    vatAmount,
  }
}

import { z } from "zod"

const decimal = /^(-?)(\d+)(?:\.(\d{1,4}))?$/
const min = -9223372036854775808n
const max = 9223372036854775807n

/** Same accepted decimal grammar and scaled range as types.ParseQuantity. */
export function isQuantityText(text: string): boolean {
  const match = decimal.exec(text)
  if (!match) return false
  const digits = (match[2] + (match[3] ?? "").padEnd(4, "0")).replace(/^0+/, "") || "0"
  if (digits.length > 19) return false
  const scaled = BigInt(match[1] + digits)
  return scaled >= min && scaled <= max
}

export const quantityInputSchema = z.union([z.string(), z.number().finite()]).refine(
  value => isQuantityText(String(value)), "Введите количество в допустимом диапазоне: не более четырёх знаков после точки",
)

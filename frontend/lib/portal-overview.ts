import { formatDecimal } from "./exchange-rates"
import { MoneyDecimal, rawMinorToMajor } from "./money"
import type { PortalChartPoint, PortalTokenBalance } from "../types/portal-api"

export type ReceiptPeriod = "7d" | "30d" | "90d"
export type ReceiptAsset = Pick<PortalChartPoint, "tokenId" | "symbol" | "network" | "decimalPlaces">

export function formatPortalAmount(raw: string, scale: number): string {
  return formatDecimal(rawMinorToMajor(raw, scale))
}
export function receiptAssets(points: PortalChartPoint[], balances: PortalTokenBalance[]): ReceiptAsset[] {
  const assets = new Map<string, ReceiptAsset>()
  for (const token of balances) assets.set(token.tokenId, {
    tokenId: token.tokenId, symbol: token.tokenSymbol, network: token.network ?? "", decimalPlaces: token.decimalPlaces,
  })
  for (const point of points) assets.set(point.tokenId, {
    tokenId: point.tokenId, symbol: point.symbol, network: point.network, decimalPlaces: point.decimalPlaces,
  })
  return [...assets.values()].sort((a, b) => (a.symbol + a.network).localeCompare(b.symbol + b.network))
}
/** UTC dates and exact integer sums. Only the normalized geometry becomes a number. */
export function receiptSeries(points: PortalChartPoint[], tokenId: string, period: ReceiptPeriod, now = new Date()) {
  const count = Number.parseInt(period)
  const end = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  const totals = new Map<string, bigint>()
  for (const point of points) {
    if (point.tokenId === tokenId) totals.set(point.day, (totals.get(point.day) ?? 0n) + BigInt(point.deposits))
  }
  const days = Array.from({ length: count }, (_, i) => {
    const day = new Date(end - (count - i - 1) * 86400000).toISOString().slice(0, 10)
    return { day, raw: (totals.get(day) ?? 0n).toString() }
  })
  const max = days.reduce((value, point) => BigInt(point.raw) > value ? BigInt(point.raw) : value, 0n)
  return {
    total: days.reduce((sum, point) => sum + BigInt(point.raw), 0n).toString(),
    max: max.toString(),
    days: days.map(point => ({ ...point, height: max === 0n ? 0 : new MoneyDecimal(point.raw).div(max.toString()).toNumber() })),
  }
}
export function receiptAxisLabel(ratio: number, max: string, scale: number) {
  if (ratio === 0) return "0"
  const value = new MoneyDecimal(rawMinorToMajor(max, scale)).mul(ratio)
  if (value.abs().gte("1000000") || value.abs().lt("0.001")) return value.toExponential(1).replace(".", ",")
  return formatDecimal(value.toSignificantDigits(3).toFixed())
}

import { test, expect } from "@playwright/test"
import { formatPortalAmount, receiptAssets, receiptAxisLabel, receiptAxisTicks, receiptSeries } from "../../configuration/crypto/lib/portal-overview"
import type { PortalChartPoint } from "../../configuration/crypto/types/portal-api"

const today = new Date("2026-10-02T23:30:00Z")
const point = (day: string, deposits: string, tokenId = "eth"): PortalChartPoint => ({
  day, deposits, tokenId, symbol: "ETH", network: tokenId, decimalPlaces: 18,
})

test("receipt totals preserve 78-digit integers, exact fractions and token isolation", () => {
  const raw = "9".repeat(78)
  const series = receiptSeries([point("2026-10-02", raw), point("2026-10-02", "2"), point("2026-10-02", "100000000", "other")], "eth", "7d", today)
  expect(series.total).toBe((BigInt(raw) + 2n).toString())
  expect(series.days.at(-1)?.height).toBe(1)
  expect(formatPortalAmount("1", 18)).toBe("0,000000000000000001")
  expect(formatPortalAmount("150000000000000000", 18)).toBe("0,15")
  expect(formatPortalAmount(raw, 18)).toContain(",999999999999999999")
  expect(receiptAxisLabel(0.5, "1", 18)).toBe("5,0e-19")
})

test("periods fill missing UTC days with zero and exclude out-of-range receipts", () => {
  for (const period of ["7d", "30d", "90d"] as const) {
    const count = Number.parseInt(period)
    const start = new Date(Date.UTC(2026, 9, 2) - (count - 1) * 86400000).toISOString().slice(0, 10)
    const series = receiptSeries([point(start, "3"), point("2026-10-02", "7"), point("2026-01-01", "999"), point("2026-10-03", "999")], "eth", period, today)
    expect(series.days).toHaveLength(count)
    expect(series.days[0]).toEqual({ day: start, raw: "3", height: 3 / 7 })
    expect(series.total).toBe("10")
    expect(series.days[1].height).toBe(0)
  }
  expect(receiptSeries([], "eth", "7d", today).days.every(day => day.height === 0)).toBe(true)
})

test("same-symbol tokens in different networks remain separate assets", () => {
  const assets = receiptAssets([point("2026-10-02", "1", "sepolia"), point("2026-10-02", "2", "mainnet")], [])
  expect(assets).toHaveLength(2)
  expect(assets.map(asset => asset.network)).toEqual(["mainnet", "sepolia"])
})

test("axis ticks land on round amounts while bars keep their exact ratio to the maximum", () => {
  // 1,234567 USDT → steps of 0,5 up to 1,5
  const { ticks, domainMax } = receiptAxisTicks("1234567", 6)
  expect(ticks.map(t => receiptAxisLabel(t, "1234567", 6))).toEqual(["0", "0,5", "1", "1,5"])
  expect(domainMax).toBeGreaterThanOrEqual(1)
  // exact multiple: no extra empty step above the tallest bar
  expect(receiptAxisTicks("400", 0).ticks.map(t => receiptAxisLabel(t, "400", 0))).toEqual(["0", "100", "200", "300", "400"])
  expect(receiptAxisTicks("0", 6)).toEqual({ ticks: [0, 1], domainMax: 1 })
})

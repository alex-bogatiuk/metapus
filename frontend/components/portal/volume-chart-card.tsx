"use client"

import { useMemo, useState } from "react"
import useSWR from "swr"
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis, type TooltipProps } from "recharts"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer } from "@/components/ui/chart"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { OverviewEmpty, OverviewError, OverviewLoading } from "./overview-states"
import { api } from "@/lib/api"
import { formatPortalAmount, receiptAssets, receiptAxisLabel, receiptSeries, type ReceiptAsset, type ReceiptPeriod } from "@/lib/portal-overview"
import type { PortalTokenBalance } from "@/types/portal-api"

const chartConfig = { height: { label: "Поступления", color: "hsl(var(--primary))" } }
function dateLabel(day: string) {
  return new Date(day + "T00:00:00Z").toLocaleDateString("ru-RU", { day: "numeric", month: "short", timeZone: "UTC" })
}
function ReceiptTooltip({ active, payload, asset }: TooltipProps<number, string> & { asset: ReceiptAsset }) {
  const point = payload?.[0]?.payload as { day: string; raw: string } | undefined
  if (!active || !point) return null
  return <div className="flex max-w-xs flex-col gap-1 rounded-lg border bg-popover p-3 text-sm text-popover-foreground shadow-md">
    <p className="text-muted-foreground">{dateLabel(point.day)} · UTC</p>
    <p className="font-medium tabular-nums [overflow-wrap:anywhere]">{formatPortalAmount(point.raw, asset.decimalPlaces)} {asset.symbol}</p>
  </div>
}

export function VolumeChartCard({ merchantId, balances = [] }: { merchantId: string | null; balances?: PortalTokenBalance[] }) {
  const [period, setPeriod] = useState<ReceiptPeriod>("30d")
  const [selectedToken, setSelectedToken] = useState("")
  const { data, error, isLoading, mutate } = useSWR(
    merchantId ? ["portal-receipts", merchantId, period] : null,
    () => api.portal.chart(period, merchantId!),
    { keepPreviousData: false, refreshInterval: 60000 },
  )
  const assets = useMemo(() => receiptAssets(data?.items ?? [], balances), [data, balances])
  const asset = assets.find(item => item.tokenId === selectedToken) ?? assets[0]
  const series = useMemo(() => receiptSeries(data?.items ?? [], asset?.tokenId ?? "", period), [data, asset?.tokenId, period])
  return <Card data-testid="receipts-card">
    <CardHeader>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-2"><CardTitle>Объём поступлений</CardTitle><CardDescription>Подтверждённые платежи до комиссии · даты UTC</CardDescription></div>
        <ToggleGroup type="single" value={period} onValueChange={value => { if (value) setPeriod(value as ReceiptPeriod) }} variant="outline" size="sm" aria-label="Период поступлений">
          <ToggleGroupItem value="7d">7 дней</ToggleGroupItem><ToggleGroupItem value="30d">30 дней</ToggleGroupItem><ToggleGroupItem value="90d">90 дней</ToggleGroupItem>
        </ToggleGroup>
      </div>
    </CardHeader>
    <CardContent className="flex min-w-0 flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-1">
          <p className="text-sm text-muted-foreground">За выбранный период</p>
          <p data-testid="receipts-total" className="text-2xl font-semibold tabular-nums [overflow-wrap:anywhere]">
            {isLoading || error || !asset ? "—" : formatPortalAmount(series.total, asset.decimalPlaces)}
            {asset && <span className="text-base font-normal text-muted-foreground"> {asset.symbol}</span>}
          </p>
        </div>
        {asset && <div className="flex w-full flex-col gap-2 sm:w-64">
          <label htmlFor="receipt-asset" className="text-sm text-muted-foreground">Актив и сеть</label>
          <Select value={asset.tokenId} onValueChange={setSelectedToken}>
            <SelectTrigger id="receipt-asset"><SelectValue /></SelectTrigger>
            <SelectContent><SelectGroup>{assets.map(item => <SelectItem key={item.tokenId} value={item.tokenId}>{item.symbol} · {item.network || "Сеть не указана"}</SelectItem>)}</SelectGroup></SelectContent>
          </Select>
        </div>}
      </div>
      {isLoading ? <OverviewLoading label="Загрузка поступлений" /> : error ? <OverviewError title="Не удалось загрузить поступления" retry={() => mutate().catch(() => undefined)} /> : !asset || series.total === "0" ? <OverviewEmpty title="За этот период поступлений нет" description="Попробуйте другой период или актив. Подтверждённые платежи появятся на графике после зачисления." /> : <>
        <ChartContainer config={chartConfig} className="h-64 w-full" aria-label={"Поступления в " + asset.symbol + ", " + asset.network}>
          <BarChart accessibilityLayer data={series.days} margin={{ left: 0, right: 8, top: 12, bottom: 4 }}>
            <CartesianGrid vertical={false} />
            <XAxis dataKey="day" tickFormatter={dateLabel} tickLine={false} axisLine={false} minTickGap={24} />
            <YAxis domain={[0, 1]} ticks={[0, 0.25, 0.5, 0.75, 1]} tickFormatter={value => receiptAxisLabel(value, series.max, asset.decimalPlaces)} tickLine={false} axisLine={false} width={66} />
            <Tooltip content={<ReceiptTooltip asset={asset} />} />
            <Bar dataKey="height" fill="var(--color-height)" radius={[4, 4, 0, 0]} maxBarSize={40} isAnimationActive={false} />
          </BarChart>
        </ChartContainer>
        <table className="sr-only" aria-label="Поступления по дням"><thead><tr><th>Дата UTC</th><th>Сумма, {asset.symbol}</th></tr></thead><tbody>{series.days.map(point => <tr key={point.day}><td>{point.day}</td><td>{formatPortalAmount(point.raw, asset.decimalPlaces)}</td></tr>)}</tbody></table>
      </>}
      <p className="text-xs text-muted-foreground">Каждый актив показан отдельно: суммы разных токенов и сетей не складываются.</p>
    </CardContent>
  </Card>
}

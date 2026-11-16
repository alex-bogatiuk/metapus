"use client"
import { BarChart, Bar, CartesianGrid, XAxis, YAxis, Tooltip } from "recharts"
import { BarChart3, Coins } from "lucide-react"
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from "@/components/ui/table"
import { ChartContainer } from "@/components/ui/chart"
import { formatPortalAmount, receiptSeries, receiptAxisLabel, receiptAxisTicks } from "@/configuration/crypto/lib/portal-overview"
import type { WidgetRenderProps } from "@/types/dashboard"
import type { AnalyticsConfig } from "@/configuration/crypto/lib/widget-configs"
import { WidgetBody, WidgetEmpty } from "@/components/dashboard/widgets/widget-state"
import { useCompactRows } from "@/components/dashboard/widgets/compact-rows"
import { useAnalytics } from "./analytics-controls"

export default function Receipts(props: WidgetRenderProps<AnalyticsConfig>) {
  const state = useAnalytics(props)
  const series = receiptSeries(state.data?.receipts ?? [], state.asset?.tokenId ?? "", props.config.period, state.data ? new Date(state.data.toDay + "T00:00:00Z") : new Date())
  const scale = state.asset?.decimalPlaces ?? 0
  const axis = receiptAxisTicks(series.max, scale)
  useCompactRows(state.data && !state.error && (!state.asset || series.total === "0") ? 2 : null)
  return <WidgetBody title="Объём поступлений" description="Подтверждённые и проведённые платежи · дни по UTC" loading={state.loading} error={state.error} retry={state.refetch} controls={state.controls}>
    {!state.asset ? <WidgetEmpty inline icon={Coins} title="Токены не настроены" action={{ label: "Открыть токены", href: "/catalogs/tokens" }} /> : <>
      <div className="mb-2"><span className="text-[26px] font-semibold leading-tight tracking-tight tabular-nums">{formatPortalAmount(series.total, scale)}</span> <span className="text-sm text-muted-foreground">{state.asset.symbol} за период</span></div>
      {series.total === "0" ? <WidgetEmpty inline icon={BarChart3} title="Поступлений за период нет" description="Здесь появятся подтверждённые платежи." />
        // The chart takes whatever height the widget has left, so a taller widget means a taller chart, not empty space.
        : <ChartContainer className="min-h-40 w-full flex-1" config={{ height: { label: "Поступления", color: "hsl(var(--chart-1))" } }} aria-label={`Поступления: ${state.asset.symbol}, ${state.asset.network}`}>
          <BarChart data={series.days} accessibilityLayer margin={{ top: 8, right: 4, left: 0, bottom: 0 }} barCategoryGap="18%">
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis dataKey="day" tickFormatter={v => `${String(v).slice(8)}.${String(v).slice(5, 7)}`} minTickGap={32} tickLine={false} axisLine={false} tickMargin={6} />
              <YAxis width={56} domain={[0, axis.domainMax]} ticks={axis.ticks} tickFormatter={v => receiptAxisLabel(Number(v), series.max, scale)} tickLine={false} axisLine={false} />
              <Tooltip cursor={{ fill: "hsl(var(--muted))" }} content={({ active, payload, label }) => active && payload?.[0] ? <div className="rounded-md border bg-popover px-3 py-2 text-popover-foreground shadow-md"><p className="text-xs text-muted-foreground">{String(label)} · UTC</p><p className="font-medium tabular-nums">{formatPortalAmount(String(payload[0].payload.raw), scale)} {state.asset?.symbol}</p></div> : null} />
              <Bar dataKey="height" fill="var(--color-height)" radius={[4, 4, 0, 0]} maxBarSize={28} />
          </BarChart>
        </ChartContainer>}
      <details className="mt-2 shrink-0 text-xs"><summary className="cursor-pointer text-muted-foreground hover:text-foreground">Данные по дням</summary><Table aria-label="Поступления по дням"><TableHeader><TableRow><TableHead>Дата (UTC)</TableHead><TableHead className="text-right">{state.asset.symbol}</TableHead></TableRow></TableHeader><TableBody>{series.days.map(d => <TableRow key={d.day}><TableCell>{d.day}</TableCell><TableCell className="text-right tabular-nums">{formatPortalAmount(d.raw, scale)}</TableCell></TableRow>)}</TableBody></Table></details>
    </>}
  </WidgetBody>
}

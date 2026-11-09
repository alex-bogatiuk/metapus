"use client"
import { BarChart, Bar, CartesianGrid, XAxis, YAxis, Tooltip } from "recharts"
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from "@/components/ui/table"
import { ChartContainer } from "@/components/ui/chart"
import { formatPortalAmount, receiptSeries, receiptAxisLabel } from "@/lib/portal-overview"
import type { WidgetRenderProps } from "@/types/dashboard"
import { WidgetBody, WidgetEmpty } from "./widget-state"
import { useAnalytics } from "./analytics-controls"

export default function Receipts(props: WidgetRenderProps<"receipts-volume">) {
  const state = useAnalytics(props)
  const series = receiptSeries(state.data?.receipts ?? [], state.asset?.tokenId ?? "", props.config.period, state.data ? new Date(state.data.toDay + "T00:00:00Z") : new Date())
  return <WidgetBody title="Объём поступлений" description="Подтверждённые и проведённые платежи · дни по UTC" loading={state.loading} error={state.error} retry={state.refetch} observedAt={state.data?.observedAt} controls={state.controls}>
    {!state.asset ? <WidgetEmpty title="Токены не настроены" /> : <><div className="mb-3 text-2xl font-semibold tabular-nums">{formatPortalAmount(series.total, state.asset.decimalPlaces)} <span className="text-sm font-normal text-muted-foreground">{state.asset.symbol}</span></div>
      {series.total === "0" ? <WidgetEmpty title="Поступлений за период нет" description="Здесь появятся подтверждённые платежи." /> : <ChartContainer className="h-56 w-full" config={{ height: { label: "Поступления", color: "hsl(var(--primary))" } }} aria-label={`Поступления: ${state.asset.symbol}, ${state.asset.network}`}><BarChart data={series.days} accessibilityLayer margin={{ top: 8, right: 8, left: 0, bottom: 0 }}><CartesianGrid vertical={false} /><XAxis dataKey="day" tickFormatter={v => `${String(v).slice(8)}.${String(v).slice(5, 7)}`} minTickGap={32} tickLine={false} axisLine={false} /><YAxis width={64} domain={[0, 1]} tickFormatter={v => receiptAxisLabel(Number(v), series.max, state.asset?.decimalPlaces ?? 0)} tickLine={false} axisLine={false} /><Tooltip content={({ active, payload, label }) => active && payload?.[0] ? <div className="rounded-md border bg-popover p-3 text-popover-foreground shadow-md"><p className="text-xs text-muted-foreground">{String(label)} · UTC</p><p className="font-medium tabular-nums">{formatPortalAmount(String(payload[0].payload.raw), state.asset?.decimalPlaces ?? 0)} {state.asset?.symbol}</p></div> : null} /><Bar dataKey="height" fill="var(--color-height)" radius={[3, 3, 0, 0]} /></BarChart></ChartContainer>}
      <details className="mt-3 text-xs"><summary className="cursor-pointer text-muted-foreground">Данные по дням</summary><Table aria-label="Поступления по дням"><TableHeader><TableRow><TableHead>Дата (UTC)</TableHead><TableHead className="text-right">{state.asset.symbol}</TableHead></TableRow></TableHeader><TableBody>{series.days.map(d => <TableRow key={d.day}><TableCell>{d.day}</TableCell><TableCell className="text-right tabular-nums">{formatPortalAmount(d.raw, state.asset?.decimalPlaces ?? 0)}</TableCell></TableRow>)}</TableBody></Table></details>
    </>}
  </WidgetBody>
}

"use client"
import { Receipt } from "lucide-react"
import { cn } from "@/lib/utils"
import type { WidgetRenderProps } from "@/types/dashboard"
import type { AnalyticsConfig } from "@/configuration/crypto/lib/widget-configs"
import { WidgetBody, WidgetEmpty } from "@/components/dashboard/widgets/widget-state"
import { useCompactRows } from "@/components/dashboard/widgets/compact-rows"
import { useAnalytics } from "./analytics-controls"

type Asset = NonNullable<ReturnType<typeof useAnalytics>["asset"]>
// Order follows the invoice outcome: settled, still moving, closed without money, rolled back.
const outcomes: { key: keyof Asset & ("paid" | "underpaid" | "awaitingConfirmation" | "pending" | "expired" | "cancelled" | "reversed"); label: string; color: string }[] = [
  { key: "paid", label: "Оплачено", color: "bg-success" },
  { key: "underpaid", label: "Недоплачено", color: "bg-warning" },
  { key: "awaitingConfirmation", label: "На подтверждении", color: "bg-chart-1" },
  { key: "pending", label: "В ожидании", color: "bg-chart-1/45" },
  { key: "expired", label: "Истекло", color: "bg-muted-foreground/45" },
  { key: "cancelled", label: "Отменено", color: "bg-muted-foreground/25" },
  { key: "reversed", label: "Сторнировано", color: "bg-destructive" },
]
const percent = (count: number, total: number) => (count / total * 100).toFixed(1).replace(".", ",") + "%"

export default function Results(props: WidgetRenderProps<AnalyticsConfig>) {
  const state = useAnalytics(props)
  useCompactRows(state.data && !state.error && !state.asset?.created ? 2 : null)
  const a = state.asset
  return <WidgetBody title="Результаты инвойсов" loading={state.loading} error={state.error} retry={state.refetch} controls={state.controls}>
    {!a || !a.created ? <WidgetEmpty inline icon={Receipt} title="Инвойсов за период нет" /> : <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-x-3">
        <p><span className="text-[26px] font-semibold leading-tight tracking-tight tabular-nums">{percent(a.paid, a.created)}</span> <span className="text-sm text-muted-foreground">оплачено</span></p>
        <p className="text-xs text-muted-foreground">Создано за период: <span className="font-medium tabular-nums text-foreground">{a.created}</span> · UTC</p>
      </div>
      <div role="img" aria-label={outcomes.map(o => `${o.label}: ${a[o.key]}`).join(", ")} className="flex h-2 gap-px overflow-hidden rounded-full bg-muted">
        {outcomes.filter(o => a[o.key] > 0).map(o => <div key={o.key} className={cn("h-full", o.color)} style={{ width: `${a[o.key] / a.created * 100}%` }} />)}
      </div>
      <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs sm:grid-cols-2">
        {outcomes.map(o => <div key={o.key} className={cn("flex items-center justify-between gap-2", !a[o.key] && "text-muted-foreground")}>
          <dt className="flex min-w-0 items-center gap-1.5"><span aria-hidden className={cn("size-2 shrink-0 rounded-full", o.color)} /><span className="truncate">{o.label}</span></dt>
          <dd className="tabular-nums">{a[o.key]} <span className="text-muted-foreground">· {percent(a[o.key], a.created)}</span></dd>
        </div>)}
      </dl>
    </div>}
  </WidgetBody>
}

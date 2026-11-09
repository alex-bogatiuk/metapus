"use client"
import type { WidgetRenderProps } from "@/types/dashboard"
import { Progress } from "@/components/ui/progress"
import { WidgetBody, WidgetEmpty } from "./widget-state"
import { useAnalytics } from "./analytics-controls"
export default function Results(props: WidgetRenderProps<"payment-results">) {
  const state = useAnalytics(props)
  const a = state.asset
  return <WidgetBody title="Результаты инвойсов" description="Текущие статусы инвойсов, созданных за период · UTC" loading={state.loading} error={state.error} retry={state.refetch} observedAt={state.data?.observedAt} controls={state.controls}>
    {!a || !a.created ? <WidgetEmpty title="Инвойсов за период нет" /> : <div className="space-y-3"><div className="flex justify-between text-sm"><span>Создано</span><strong className="tabular-nums">{a.created}</strong></div>{[["Оплачено", a.paid], ["Недоплачено", a.underpaid], ["На подтверждении", a.awaitingConfirmation], ["В ожидании", a.pending], ["Истекло", a.expired], ["Отменено", a.cancelled], ["Сторнировано", a.reversed]].map(([label, count]) => <div key={String(label)}><div className="mb-1 flex justify-between text-xs"><span>{label}</span><span className="tabular-nums">{count} · {(Number(count) / a.created * 100).toFixed(1).replace(".", ",")}%</span></div><Progress value={Number(count) / a.created * 100} className="h-1.5" aria-label={String(label)} /></div>)}</div>}
  </WidgetBody>
}

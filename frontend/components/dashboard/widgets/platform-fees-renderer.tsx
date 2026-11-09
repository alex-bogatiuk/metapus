"use client"
import { formatPortalAmount } from "@/lib/portal-overview"
import type { WidgetRenderProps } from "@/types/dashboard"
import { WidgetBody, WidgetEmpty } from "./widget-state"
import { useAnalytics } from "./analytics-controls"
export default function Fees(props: WidgetRenderProps<"platform-fees">) {
  const state = useAnalytics(props)
  return <WidgetBody title="Комиссии платформы" description="Комиссии за обработку за вычетом сторно · UTC" loading={state.loading} error={state.error} retry={state.refetch} observedAt={state.data?.observedAt} controls={state.controls}>
    {!state.asset ? <WidgetEmpty title="Токены не настроены" /> : <div className="text-2xl font-semibold tabular-nums">{formatPortalAmount(state.asset.feesMinor, state.asset.decimalPlaces)} <span className="text-sm font-normal text-muted-foreground">{state.asset.symbol}</span></div>}
  </WidgetBody>
}

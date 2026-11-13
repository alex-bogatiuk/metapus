"use client"
import { Coins } from "lucide-react"
import { formatPortalAmount } from "@/lib/portal-overview"
import type { WidgetRenderProps } from "@/types/dashboard"
import { WidgetBody, WidgetEmpty } from "./widget-state"
import { useAnalytics } from "./analytics-controls"
export default function Fees(props: WidgetRenderProps<"platform-fees">) {
  const state = useAnalytics(props)
  return <WidgetBody title="Комиссии платформы" description="Комиссии за обработку за вычетом сторно · UTC" loading={state.loading} error={state.error} retry={state.refetch} controls={state.controls}>
    {!state.asset ? <WidgetEmpty icon={Coins} title="Токены не настроены" action={{ label: "Открыть токены", href: "/catalogs/tokens" }} /> : <div className="text-3xl font-semibold tracking-tight tabular-nums">{formatPortalAmount(state.asset.feesMinor, state.asset.decimalPlaces)} <span className="text-sm font-normal text-muted-foreground">{state.asset.symbol}</span></div>}
  </WidgetBody>
}

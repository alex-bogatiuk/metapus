"use client"
import { Coins } from "lucide-react"
import { formatPortalAmount } from "@/configuration/crypto/lib/portal-overview"
import type { WidgetRenderProps } from "@/types/dashboard"
import { WidgetBody, WidgetEmpty } from "./widget-state"
import { useCompactRows } from "./compact-rows"
import { useAnalytics } from "./analytics-controls"
export default function Fees(props: WidgetRenderProps<"platform-fees">) {
  const state = useAnalytics(props)
  useCompactRows(state.data && !state.error && !state.asset ? 2 : null)
  return <WidgetBody title="Комиссии платформы" description="Комиссии за обработку за вычетом сторно · UTC" loading={state.loading} error={state.error} retry={state.refetch} controls={state.controls}>
    {!state.asset ? <WidgetEmpty inline icon={Coins} title="Токены не настроены" action={{ label: "Открыть токены", href: "/catalogs/tokens" }} /> : <div className="text-[26px] font-semibold leading-tight tracking-tight tabular-nums">{formatPortalAmount(state.asset.feesMinor, state.asset.decimalPlaces)} <span className="text-sm font-normal text-muted-foreground">{state.asset.symbol}</span></div>}
  </WidgetBody>
}

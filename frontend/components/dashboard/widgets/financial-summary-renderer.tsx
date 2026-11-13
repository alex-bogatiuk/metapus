"use client"
import { api } from "@/lib/api"
import { useWidgetData } from "@/hooks/useWidgetData"
import { formatPortalAmount } from "@/lib/portal-overview"
import { formatDecimal } from "@/lib/exchange-rates"
import { observedTime } from "@/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import { Wallet } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { WidgetBody, WidgetEmpty } from "./widget-state"

export default function Finance({ config, isEditMode }: WidgetRenderProps<"financial-summary">) {
  const state = useWidgetData(api.dashboard.finance, { key: "widget:admin-finance", isEditMode, pollInterval: 60_000 })
  const balance = state.data?.[config.view]
  const bucket = balance?.valuation.total
  const title = config.view === "wallets" ? "Средства на кошельках" : "Обязательства перед мерчантами"
  return <WidgetBody title={title} description={config.view === "wallets" ? "Остатки по учётному регистру" : "Учётные остатки мерчантов"} loading={state.loading} error={state.error} retry={state.refetch}>
    {balance && <><div className="mb-1 flex flex-wrap items-baseline gap-2">{bucket?.amount == null
        ? <span className="text-lg font-medium text-muted-foreground">Нет оценки в {balance.baseCurrency.code}</span>
        : <><span className="text-3xl font-semibold tracking-tight tabular-nums">{formatDecimal(bucket.amount)}</span><span className="text-sm text-muted-foreground">{balance.baseCurrency.code} · оценка</span>{bucket.coverage !== "complete" && <Badge variant="secondary">{bucket.coverage === "partial" ? "Частичная оценка" : "Нет оценки"}</Badge>}</>}</div>
      <p className="mb-2 text-xs text-muted-foreground">{balance.source?.name ?? "Источник курса не настроен"} · {observedTime(balance.evaluatedAt)}{balance.freshUntil && ` · действует до ${observedTime(balance.freshUntil)}`}</p>
      {!state.data?.assets.length ? <WidgetEmpty icon={Wallet} title="Остатков нет" /> : <div className="divide-y">{state.data.assets.map(a => <div key={a.tokenId} className="flex flex-wrap items-center justify-between gap-2 py-2"><div><span className="font-medium">{a.symbol}</span><span className="ml-2 text-xs text-muted-foreground">{a.network}</span></div><span className="tabular-nums">{formatPortalAmount(config.view === "wallets" ? a.walletMinor : a.merchantMinor, a.decimalPlaces)}</span></div>)}</div>}
      {!!bucket?.unvaluedCount && <p className="mt-2 text-xs text-muted-foreground">Без актуального курса: {bucket.unvaluedCount}. Эти остатки не включены в оценку.</p>}
    </>}
  </WidgetBody>
}

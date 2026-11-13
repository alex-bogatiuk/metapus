"use client"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useWidgetData } from "@/hooks/useWidgetData"
import { formatPortalAmount } from "@/lib/portal-overview"
import { formatDecimal } from "@/lib/exchange-rates"
import { observedTime } from "@/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import { Wallet } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { FooterLink, WidgetBody, WidgetEmpty } from "./widget-state"

// Shares are told apart by lightness, not only hue, so they read in both themes and for colour-blind people.
const shades = ["bg-chart-1", "bg-chart-1/55", "bg-chart-1/30", "bg-chart-2", "bg-chart-2/50", "bg-chart-3"]

export default function Finance({ config, isEditMode }: WidgetRenderProps<"financial-summary">) {
  const state = useWidgetData(api.dashboard.finance, { key: "widget:admin-finance", isEditMode, pollInterval: 60_000 })
  const balance = state.data?.[config.view]
  const bucket = balance?.valuation.total
  const assets = state.data?.assets ?? []
  const title = config.view === "wallets" ? "Средства на кошельках" : "Обязательства перед мерчантами"

  // Each token's share of the valued total; only drawn when there is something to compare.
  const valued = new Map((balance?.byToken ?? []).flatMap(t => {
    const value = Number(t.valuation.total)
    return t.valuation.total != null && Number.isFinite(value) && value > 0 ? [[t.tokenId, value] as const] : []
  }))
  const valuedTotal = [...valued.values()].reduce((sum, v) => sum + v, 0)
  const showShares = valued.size > 1 && valuedTotal > 0
  const shade = (tokenId: string) => shades[[...valued.keys()].indexOf(tokenId) % shades.length]

  return <WidgetBody title={title} description={config.view === "wallets" ? "Остатки по учётному регистру" : "Учётные остатки мерчантов"}
    loading={state.loading} error={state.error} retry={state.refetch}
    action={balance && <Badge variant="outline">{balance.baseCurrency.code}</Badge>}
    flush
    footer={balance && <>
      <span>{bucket?.unvaluedCount ? `Без актуального курса: ${bucket.unvaluedCount}. Эти остатки не включены в оценку.` : "Все остатки оценены"}</span>
      {!isEditMode && <FooterLink href="/finance/exchange-rates">Курсы валют</FooterLink>}
    </>}
  >
    {balance && <>
      <div className="px-4 pb-3">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">{bucket?.amount == null
          ? <span className="text-lg font-medium text-muted-foreground">Нет оценки в {balance.baseCurrency.code}</span>
          : <><span className="text-[26px] font-semibold leading-tight tracking-tight tabular-nums">{formatDecimal(bucket.amount)}</span><span className="text-sm text-muted-foreground">{balance.baseCurrency.code}</span>{bucket.coverage !== "complete" && <Badge variant="warning">{bucket.coverage === "partial" ? "Частичная оценка" : "Нет оценки"}</Badge>}</>}
        </div>
        <p className="mt-1 text-xs text-muted-foreground">{balance.source?.name ?? "Источник курса не настроен"} · {observedTime(balance.evaluatedAt)}{balance.freshUntil && ` · действует до ${observedTime(balance.freshUntil)}`}</p>
        {showShares && <div role="img" aria-label={`Доли в оценке: ${assets.filter(a => valued.has(a.tokenId)).map(a => `${a.symbol} ${a.network} ${Math.round(valued.get(a.tokenId)! / valuedTotal * 100)}%`).join(", ")}`} className="mt-3 flex h-1.5 gap-0.5 overflow-hidden rounded-full">
          {[...valued.entries()].map(([tokenId, value]) => <span key={tokenId} className={cn("h-full", shade(tokenId))} style={{ width: `${value / valuedTotal * 100}%` }} />)}
        </div>}
      </div>
      {!assets.length ? <WidgetEmpty icon={Wallet} title="Остатков нет" /> : <ul className="divide-y border-t">{assets.map(a => <li key={a.tokenId} className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 px-4 py-2.5">
        <span className="flex min-w-0 items-center gap-2">
          {showShares && <span aria-hidden className={cn("size-2 shrink-0 rounded-[3px]", valued.has(a.tokenId) ? shade(a.tokenId) : "bg-muted")} />}
          <span className="font-medium">{a.symbol}</span>
          <span className="truncate text-xs text-muted-foreground">{a.network}</span>
        </span>
        <span className="tabular-nums">{formatPortalAmount(config.view === "wallets" ? a.walletMinor : a.merchantMinor, a.decimalPlaces)}</span>
      </li>)}</ul>}
    </>}
  </WidgetBody>
}

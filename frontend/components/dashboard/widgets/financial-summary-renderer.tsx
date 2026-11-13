"use client"
import { api } from "@/lib/api"
import { useWidgetData } from "@/hooks/useWidgetData"
import { formatPortalAmount } from "@/lib/portal-overview"
import { formatDecimal } from "@/lib/exchange-rates"
import { observedTime } from "@/lib/admin-dashboard"
import { paletteColor, resolvePaletteKey } from "@/lib/palette"
import type { WidgetRenderProps } from "@/types/dashboard"
import { Wallet } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { FooterLink, WidgetBody, WidgetEmpty } from "./widget-state"

// A token takes its network's colour; tokens of one network are told apart by lightness,
// so the bar reads by network first and by token second, in both themes.
const shadeAlphas = [1, 0.6, 0.35, 0.2]

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

  // Valued tokens grouped by network, so a network's shades sit side by side in the bar.
  const byNetwork = new Map<string, typeof assets>()
  for (const a of assets) if (valued.has(a.tokenId)) byNetwork.set(a.networkId, [...(byNetwork.get(a.networkId) ?? []), a])
  const segments = [...byNetwork.values()].flatMap(group => group.map((a, i) =>
    ({ tokenId: a.tokenId, color: paletteColor(resolvePaletteKey(a.networkColor, a.networkId), shadeAlphas[Math.min(i, shadeAlphas.length - 1)]) })))
  const shade = new Map(segments.map(s => [s.tokenId, s.color]))

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
          {segments.map(({ tokenId, color }) => <span key={tokenId} className="h-full" style={{ width: `${valued.get(tokenId)! / valuedTotal * 100}%`, backgroundColor: color }} />)}
        </div>}
      </div>
      {!assets.length ? <WidgetEmpty icon={Wallet} title="Остатков нет" /> : <ul className="divide-y border-t">{assets.map(a => <li key={a.tokenId} className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 px-4 py-2.5">
        <span className="flex min-w-0 items-center gap-2">
          {showShares && <span aria-hidden className="size-2 shrink-0 rounded-[3px] bg-muted" style={shade.has(a.tokenId) ? { backgroundColor: shade.get(a.tokenId) } : undefined} />}
          <span className="font-medium">{a.symbol}</span>
          <span className="truncate text-xs text-muted-foreground">{a.network}</span>
        </span>
        <span className="tabular-nums">{formatPortalAmount(config.view === "wallets" ? a.walletMinor : a.merchantMinor, a.decimalPlaces)}</span>
      </li>)}</ul>}
    </>}
  </WidgetBody>
}

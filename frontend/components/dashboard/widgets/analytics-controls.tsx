"use client"
import { api } from "@/lib/api"
import { useWidgetData } from "@/hooks/useWidgetData"
import type { WidgetRenderProps } from "@/types/dashboard"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

export function useAnalytics<T extends "receipts-volume" | "platform-fees" | "payment-results">(props: WidgetRenderProps<T>) {
  const state = useWidgetData(() => api.dashboard.analytics(props.config.period), { key: `widget:admin-analytics:${props.config.period}`, isEditMode: props.isEditMode, pollInterval: 60_000 })
  const assets = state.data?.assets ?? []
  const asset = assets.find(a => a.tokenId === props.config.tokenId) ?? assets[0]
  const controls = <div className="flex flex-wrap items-center gap-1.5">
    <ToggleGroup type="single" variant="outline" size="sm" value={props.config.period} aria-label="Период аналитики" onValueChange={value => { if (value === "7d" || value === "30d" || value === "90d") props.onConfigChange({ ...props.config, period: value }) }}>
      {(["7d", "30d", "90d"] as const).map(p => <ToggleGroupItem key={p} value={p} className="h-7 px-2 text-xs">{p.slice(0, -1)} д</ToggleGroupItem>)}
    </ToggleGroup>
    {!!assets.length && <Select value={asset?.tokenId} onValueChange={tokenId => props.onConfigChange({ ...props.config, tokenId })}><SelectTrigger className="h-7 w-auto max-w-full gap-1 px-2 text-xs" aria-label="Токен и сеть"><SelectValue /></SelectTrigger><SelectContent><SelectGroup>{assets.map(a => <SelectItem key={a.tokenId} value={a.tokenId}>{a.symbol} · {a.network}</SelectItem>)}</SelectGroup></SelectContent></Select>}
  </div>
  return { ...state, asset, controls }
}

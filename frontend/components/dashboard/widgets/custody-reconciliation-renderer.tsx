"use client"
import Link from "next/link"
import { api } from "@/lib/api"
import { useWidgetData } from "@/hooks/useWidgetData"
import { formatPortalAmount } from "@/lib/portal-overview"
import { observedTime } from "@/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import { ShieldCheck } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from "@/components/ui/table"
import { WidgetBody, WidgetEmpty } from "./widget-state"

export default function Reconciliation({ config, isEditMode }: WidgetRenderProps<"custody-reconciliation">) {
  const state = useWidgetData(api.dashboard.finance, { key: "widget:admin-finance", isEditMode, pollInterval: 60_000 })
  const assets = [...(state.data?.assets ?? [])].sort((a, b) => {
    const priority = (x: typeof a) => x.mismatchedWalletCount > 0 ? 0 : x.deltaMinor == null ? 1 : 2
    return priority(a) - priority(b)
  })
  return <WidgetBody title="Сверка кошельков" description="Учёт и блокчейн на момент последних проверок. Самая ранняя проверка указана для каждого токена." loading={state.loading} error={state.error} retry={state.refetch}>
    {!assets.length ? <WidgetEmpty icon={ShieldCheck} title="Нет данных для сверки" description="Сверка появится после первой проверки кошельков." action={{ label: "Открыть управление сетями", href: "/admin/crypto-networks" }} /> : <Table><TableHeader><TableRow><TableHead>Токен / сеть</TableHead><TableHead className="text-right">Учёт</TableHead><TableHead className="text-right">Блокчейн</TableHead><TableHead className="text-right">Расхождение</TableHead><TableHead>Проверка</TableHead></TableRow></TableHeader><TableBody>{assets.slice(0, config.limit).map(a => <TableRow key={a.tokenId}><TableCell><span className="font-medium">{a.symbol}</span><div className="text-xs text-muted-foreground">{a.network}</div></TableCell><TableCell className="text-right tabular-nums">{a.reconciledLedgerMinor == null ? "—" : formatPortalAmount(a.reconciledLedgerMinor, a.decimalPlaces)}</TableCell><TableCell className="text-right tabular-nums">{a.chainMinor == null ? "—" : formatPortalAmount(a.chainMinor, a.decimalPlaces)}</TableCell><TableCell className="text-right tabular-nums">{a.deltaMinor == null ? "—" : formatPortalAmount(a.deltaMinor, a.decimalPlaces)}</TableCell><TableCell><Badge variant={a.mismatchedWalletCount > 0 ? "destructive" : "secondary"}>{a.mismatchedWalletCount > 0 ? `Расхождения: ${a.mismatchedWalletCount}` : a.deltaMinor == null ? "Нет полной сверки" : a.reconciledAt && Date.parse(state.data?.observedAt ?? "") - Date.parse(a.reconciledAt) > 86400000 ? "Проверка старше суток" : "Совпадает"}</Badge><div className="mt-1 text-xs text-muted-foreground">{a.checkedWalletCount}/{a.walletCount} кошельков · {observedTime(a.reconciledAt)}</div></TableCell></TableRow>)}</TableBody></Table>}
    <div className="mt-3 flex flex-wrap justify-between gap-2 text-xs text-muted-foreground"><span>Показаны {Math.min(config.limit, assets.length)} из {assets.length} токенов</span><Link className="underline underline-offset-4" href="/admin/crypto-networks">Открыть управление сетями</Link></div>
  </WidgetBody>
}

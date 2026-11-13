"use client"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useWidgetData } from "@/hooks/useWidgetData"
import { formatPortalAmount } from "@/lib/portal-overview"
import { observedTime } from "@/lib/admin-dashboard"
import type { WidgetRenderProps } from "@/types/dashboard"
import { ShieldCheck } from "lucide-react"
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from "@/components/ui/table"
import { FooterLink, StatusBadge, WidgetBody, WidgetEmpty } from "./widget-state"
import { useCompactRows } from "./compact-rows"

export default function Reconciliation({ config, isEditMode }: WidgetRenderProps<"custody-reconciliation">) {
  const state = useWidgetData(api.dashboard.finance, { key: "widget:admin-finance", isEditMode, pollInterval: 60_000 })
  const assets = [...(state.data?.assets ?? [])].sort((a, b) => {
    const priority = (x: typeof a) => x.mismatchedWalletCount > 0 ? 0 : x.deltaMinor == null ? 1 : 2
    return priority(a) - priority(b)
  })
  useCompactRows(state.data && !state.error && assets.length === 0 ? 1 : null)
  const stale = (reconciledAt: string | null) => !!reconciledAt && Date.parse(state.data?.observedAt ?? "") - Date.parse(reconciledAt) > 86400000
  return <WidgetBody title="Сверка кошельков" description="Учёт и блокчейн на момент последних проверок. Самая ранняя проверка указана для каждого токена."
    loading={state.loading} error={state.error} retry={state.refetch}
    flush={!!assets.length}
    footer={!!assets.length && <>
      <span>Показаны {Math.min(config.limit, assets.length)} из {assets.length} токенов</span>
      {!isEditMode && <FooterLink href="/admin/crypto-networks">Открыть управление сетями</FooterLink>}
    </>}
  >
    {!assets.length ? <WidgetEmpty inline icon={ShieldCheck} title="Нет данных для сверки" description="Сверка появится после первой проверки кошельков." action={{ label: "Открыть управление сетями", href: "/admin/crypto-networks" }} /> : <Table>
      <TableHeader><TableRow><TableHead>Токен / сеть</TableHead><TableHead className="text-right">Учёт</TableHead><TableHead className="text-right">Блокчейн</TableHead><TableHead className="text-right">Расхождение</TableHead><TableHead>Проверка</TableHead></TableRow></TableHeader>
      <TableBody>{assets.slice(0, config.limit).map(a => {
        const mismatch = a.mismatchedWalletCount > 0
        return <TableRow key={a.tokenId}>
          <TableCell><span className="font-medium">{a.symbol}</span><div className="mt-0.5 text-xs text-muted-foreground">{a.network}</div></TableCell>
          <TableCell className={cn("text-right tabular-nums", a.reconciledLedgerMinor == null && "text-muted-foreground")}>{a.reconciledLedgerMinor == null ? "—" : formatPortalAmount(a.reconciledLedgerMinor, a.decimalPlaces)}</TableCell>
          <TableCell className={cn("text-right tabular-nums", a.chainMinor == null && "text-muted-foreground")}>{a.chainMinor == null ? "—" : formatPortalAmount(a.chainMinor, a.decimalPlaces)}</TableCell>
          <TableCell className={cn("text-right tabular-nums", a.deltaMinor == null ? "text-muted-foreground" : mismatch && "font-medium text-destructive")}>{a.deltaMinor == null ? "—" : formatPortalAmount(a.deltaMinor, a.decimalPlaces)}</TableCell>
          <TableCell>
            {mismatch ? <StatusBadge tone="destructive">Расхождения: {a.mismatchedWalletCount}</StatusBadge>
              : a.deltaMinor == null ? <StatusBadge tone="neutral">Нет полной сверки</StatusBadge>
              : stale(a.reconciledAt) ? <StatusBadge tone="warning">Проверка старше суток</StatusBadge>
              : <StatusBadge tone="success">Совпадает</StatusBadge>}
            <div className="mt-1 text-xs text-muted-foreground">{a.checkedWalletCount}/{a.walletCount} кошельков · {observedTime(a.reconciledAt)}</div>
          </TableCell>
        </TableRow>
      })}</TableBody>
    </Table>}
  </WidgetBody>
}

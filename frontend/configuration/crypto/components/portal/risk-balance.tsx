"use client"

import { useEffect } from "react"
import { ChevronDown, RefreshCw } from "lucide-react"
import { useRiskBalance } from "@/hooks/use-risk-balance"
import { useUserPrefsStore } from "@/stores/useUserPrefsStore"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow, TableFooter } from "@/components/ui/table"
import { OverviewEmpty, OverviewError, OverviewLoading } from "./overview-states"
import { fmtRawAmount, formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { AmlMerchantRiskBalance } from "@/types/generated/http-contracts"

const classLabels: Record<string, string> = { low: "Низкий", medium: "Средний", high: "Высокий", unknown: "Не определён" }
const reasonLabels: Record<string, string> = {
  routing_not_enabled: "Оценка риска для этого актива пока не включена.",
  ledger_reconciliation_required: "Данные финансового учёта требуют сверки.",
  withdrawal_allocation_required: "Для незавершённой заявки ещё не определён точный состав источников.",
  withdrawal_reconciliation_required: "Для незавершённой заявки не подтверждено списание с баланса. Требуется сверка.",
  source_reconciliation_required: "Суммы источников и финансового учёта требуют сверки.",
  source_allocation_incomplete: "Часть средств ещё не привязана к подтверждённым источникам.",
}

function AssetSummary({ item }: { item: AmlMerchantRiskBalance }) {
  const amount = (value: string) => `${fmtRawAmount(value, item.decimalPlaces)} ${item.symbol}`
  const high = item.buckets.find(bucket => bucket.riskClass === "high")?.total ?? "0"
  const unknown = item.buckets.find(bucket => bucket.riskClass === "unknown")?.total ?? "0"
  return <section aria-label={`${item.symbol} · ${item.network}`} className="flex flex-col gap-2">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-wrap items-center gap-2"><span className="font-medium">{item.symbol}</span><Badge variant="outline">{item.network}</Badge></div>
      <span className="font-medium tabular-nums">Всего: {amount(item.total)}</span>
    </div>
    <p className="text-sm text-muted-foreground tabular-nums">Свободно: {amount(item.free)} · В заявках: {amount(item.reserved)} · Требует проверки: {amount(item.technicalHold)}</p>
    {(item.hasHighExposure || unknown !== "0" || item.technicalHold !== "0") && <Alert>
      <AlertDescription className="flex flex-col gap-1">
        {item.hasHighExposure && <p>{high !== "0" ? `Высокий риск: ${amount(high)}.` : "Есть источники с известным высоким риском."}</p>}
        {unknown !== "0" && <p>Риск не определён: {amount(unknown)}.</p>}
        {item.reason !== "source_allocation_complete" && <p>{reasonLabels[item.reason] ?? "Для разбивки по риску требуется проверка источников."}</p>}
        {item.technicalHold !== "0" && item.reason === "source_allocation_complete" && <p>Средства удерживаются активными операциями. Дождитесь их завершения.</p>}
        {item.pendingRestoration !== "0" && <p>Ожидает восстановления после закрытия заявки: {amount(item.pendingRestoration)}.</p>}
      </AlertDescription>
    </Alert>}
    {item.pendingChain !== "0" && <p className="text-xs text-muted-foreground">Ожидает зачисления до комиссии: {amount(item.pendingChain)}. В сумму выше не включено.</p>}
  </section>
}

const riskBreakdownSection = "portalRiskBreakdown"

export function RiskBalance({ merchantId }: { merchantId: string }) {
  const { data, error, isLoading, isValidating, expired, mutate } = useRiskBalance(merchantId)
  const expanded = useUserPrefsStore(state => state.interface.expandedSections?.[riskBreakdownSection] ?? false)
  const loaded = useUserPrefsStore(state => state.isLoaded)
  const loadPreferences = useUserPrefsStore(state => state.loadPreferences)
  const updateInterface = useUserPrefsStore(state => state.updateInterface)
  useEffect(() => { if (!loaded) void loadPreferences() }, [loaded, loadPreferences])
  const refresh = () => { void mutate().catch(() => undefined) }
  return <Collapsible open={expanded} onOpenChange={open => updateInterface({ expandedSections: { ...useUserPrefsStore.getState().interface.expandedSections, [riskBreakdownSection]: open } })}>
    <Card data-testid="risk-balance">
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-col gap-2"><CardTitle>Риск средств</CardTitle><CardDescription>Зачисленные средства после комиссии, включая незавершённые заявки на вывод</CardDescription></div>
          <div className="flex items-center gap-2">
            <CollapsibleTrigger asChild><Button variant="outline" size="sm">По риску<ChevronDown data-icon="inline-end" className={cn(expanded && "rotate-180")} /></Button></CollapsibleTrigger>
            <Button variant="ghost" size="icon" disabled={isValidating} aria-label="Обновить риск средств" onClick={refresh}><RefreshCw data-icon="inline-start" /></Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {!data ? isLoading ? <OverviewLoading label="Загрузка риска средств" /> : <OverviewError title="Не удалось загрузить риск средств" retry={refresh} /> : <>
          {(expired || error) && <Alert role="status"><AlertDescription>{expired ? "Снимок риска устарел. Обновите данные, чтобы увидеть актуальную разбивку." : "Не удалось обновить риск. Показан последний полученный снимок."}</AlertDescription></Alert>}
          {data.items.length === 0 && <OverviewEmpty title="Нет зачисленных средств" description="Разбивка появится после поступления средств на баланс." />}
          {!expired && data.items.map(item => <AssetSummary key={item.tokenId} item={item} />)}
          {expired && data.items.some(item => item.hasHighExposure) && <Alert><AlertDescription>В последнем снимке были источники с высоким риском. Истечение снимка не снимает это ограничение.</AlertDescription></Alert>}
          <CollapsibleContent className="flex flex-col gap-5">
            {!expired && data.items.map(item => <section key={item.tokenId} className="flex min-w-0 flex-col gap-2">
              <h3 className="text-sm font-medium">{item.symbol} · {item.network}</h3>
              <Table aria-label={`Баланс по риску · ${item.symbol} · ${item.network}`}>
                <TableHeader><TableRow><TableHead>Класс</TableHead><TableHead className="text-right">Всего</TableHead><TableHead className="text-right">Свободно</TableHead><TableHead className="text-right">В заявках</TableHead><TableHead className="text-right">Требует проверки</TableHead></TableRow></TableHeader>
                <TableBody>{item.buckets.map(bucket => <TableRow key={bucket.riskClass}>
                  <TableCell>{classLabels[bucket.riskClass] ?? "Не определён"}</TableCell>
                  {(["total", "free", "reserved", "technicalHold"] as const).map(key => <TableCell key={key} className="text-right tabular-nums">{fmtRawAmount(bucket[key], item.decimalPlaces)}</TableCell>)}
                </TableRow>)}</TableBody>
                <TableFooter><TableRow><TableCell>Итого, {item.symbol}</TableCell>{(["total", "free", "reserved", "technicalHold"] as const).map(key => <TableCell key={key} className="text-right tabular-nums">{fmtRawAmount(item[key], item.decimalPlaces)}</TableCell>)}</TableRow></TableFooter>
              </Table>
            </section>)}
          </CollapsibleContent>
        </>}
      </CardContent>
      <CardFooter className="flex flex-col items-start gap-2">
        <p className="text-xs text-muted-foreground">«Свободно» означает отсутствие резервов и технического удержания. Возможность вывода проверяется отдельно.</p>
        {data && <p className="text-xs text-muted-foreground">Снимок на {formatDateTime(data.asOf)}. Суммы разных активов и сетей не складываются.</p>}
      </CardFooter>
    </Card>
  </Collapsible>
}

"use client"

import Link from "next/link"
import { ArrowUpRight, Plus, RefreshCw, Info } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { QuoteFacts } from "@/components/exchange-rates/quote-facts"
import { OverviewEmpty } from "./overview-states"
import { formatDecimal, rateReasonLabels, rateTime } from "@/lib/exchange-rates"
import type { PortalBalanceResponse, PortalTokenBalance, ValuationBucket } from "@/types/portal-api"

export type BalanceCalculation = { data: PortalBalanceResponse; tokens: PortalTokenBalance[] }

function Coverage({ bucket }: { bucket: ValuationBucket }) {
  if (bucket.coverage === "complete") return null
  return <p className="text-sm text-muted-foreground">
    {bucket.coverage === "partial" ? "Оценена часть баланса" : "Оценка недоступна"} · Без оценки: {bucket.unvaluedCount}
  </p>
}

export function BalanceOverview({
  data, expired, refreshFailed, refreshing, canCreateInvoice, canWithdraw, refresh, showDetails,
}: {
  data: PortalBalanceResponse
  expired: boolean
  refreshFailed: boolean
  refreshing: boolean
  canCreateInvoice: boolean
  canWithdraw: boolean
  refresh: () => void
  showDetails: (details: BalanceCalculation) => void
}) {
  return <Card>
    <CardHeader>
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-2">
          <CardTitle>Общий баланс</CardTitle>
          <CardDescription>Средства на счетах мерчанта</CardDescription>
        </div>
        <Button variant="ghost" size="icon" aria-label="Обновить баланс" disabled={refreshing} onClick={() => { void refresh() }}>
          <RefreshCw data-icon="inline-start" />
        </Button>
      </div>
    </CardHeader>
    <CardContent className="flex flex-col gap-6">
      <div className="flex flex-col justify-between gap-6 lg:flex-row lg:items-center">
        <div className="flex min-w-0 flex-col gap-2">
          <p data-testid="overview-total" className="text-4xl font-semibold tracking-tight tabular-nums [overflow-wrap:anywhere] sm:text-5xl">
            {formatDecimal(data.valuation.total.amount, data.baseCurrency.decimalPlaces)}{" "}
            <span className="text-lg font-normal text-muted-foreground">{data.baseCurrency.code}</span>
          </p>
          <Coverage bucket={data.valuation.total} />
        </div>
        <div className="flex flex-wrap gap-2">
          {canWithdraw && <Button asChild><Link href="/portal/withdrawals/new"><ArrowUpRight data-icon="inline-start" />Создать заявку на вывод</Link></Button>}
          {canCreateInvoice && <Button variant="outline" asChild><Link href="/portal/invoices/new"><Plus data-icon="inline-start" />Создать инвойс</Link></Button>}
        </div>
      </div>
      <dl className="grid gap-6 border-t pt-5 sm:grid-cols-2">
        {(["available", "pending"] as const).map(key => <div key={key} className="flex min-w-0 flex-col gap-1.5">
          <dt className="text-sm text-muted-foreground">{key === "available" ? "Доступно к выводу" : "Ожидает зачисления"}</dt>
          <dd className="text-xl font-medium tabular-nums [overflow-wrap:anywhere]">
            {formatDecimal(data.valuation[key].amount, data.baseCurrency.decimalPlaces)}{" "}
            <span className="text-sm text-muted-foreground">{data.baseCurrency.code}</span>
          </dd>
          <Coverage bucket={data.valuation[key]} />
          {key === "pending" && <p className="text-xs text-muted-foreground">Платежи до комиссии, которые ещё не зачислены на баланс</p>}
        </div>)}
      </dl>
      {(expired || refreshFailed) && <Alert role="status"><AlertDescription>
        {expired
          ? "Показанная оценка устарела. " + (refreshFailed ? "Не удалось обновить баланс." : "Обновляем баланс…")
          : "Не удалось обновить баланс. Показаны последние полученные данные."}
      </AlertDescription></Alert>}
    </CardContent>
    <CardFooter className="flex flex-wrap justify-between gap-2">
      <p className="text-xs text-muted-foreground">{data.source?.name ?? "Источник курса не выбран"} · Оценка на {rateTime(data.evaluatedAt)}</p>
      <Button variant="ghost" size="sm" onClick={() => showDetails({ data, tokens: data.byToken })}><Info data-icon="inline-start" />Как рассчитан баланс</Button>
    </CardFooter>
  </Card>
}

export function BalanceAssets({ data, showDetails }: { data: PortalBalanceResponse; showDetails: (details: BalanceCalculation) => void }) {
  return <Card>
    <CardHeader><CardTitle>Активы</CardTitle><CardDescription>Точные суммы по токенам и сетям. Оценка в {data.baseCurrency.code} зависит от курса.</CardDescription></CardHeader>
    <CardContent>
      {data.byToken.length === 0 ? <OverviewEmpty title="На балансе пока нет средств" description="Активы появятся здесь после первого платежа." /> : <Table>
        <TableHeader><TableRow>
          <TableHead>Актив / сеть</TableHead><TableHead className="text-right">Доступно</TableHead>
          <TableHead className="text-right">Ожидает зачисления</TableHead><TableHead className="text-right">Всего</TableHead>
          <TableHead className="text-right">Оценка, {data.baseCurrency.code}</TableHead>
        </TableRow></TableHeader>
        <TableBody>{data.byToken.map(token => <TableRow key={token.tokenId}>
          <TableCell>
            <Button variant="link" className="h-auto justify-start p-0" onClick={() => showDetails({ data, tokens: [token] })}>{token.tokenSymbol}</Button>
            <p className="mt-1 text-xs text-muted-foreground">{token.network || "Сеть не указана"}</p>
          </TableCell>
          <TableCell className="text-right tabular-nums">{formatDecimal(token.availableHuman)}</TableCell>
          <TableCell className="text-right tabular-nums">{formatDecimal(token.pendingHuman)}</TableCell>
          <TableCell className="text-right font-medium tabular-nums">{formatDecimal(token.humanAmount)}</TableCell>
          <TableCell className="text-right">
            <Button variant="ghost" size="sm" className="h-auto whitespace-normal text-right tabular-nums" aria-label={"О курсе " + token.tokenSymbol + (token.network ? " · " + token.network : "")} onClick={() => showDetails({ data, tokens: [token] })}>
              {token.valuation.total === null ? (rateReasonLabels[token.valuation.reason] ?? "Оценка недоступна") : formatDecimal(token.valuation.total, data.baseCurrency.decimalPlaces)}
            </Button>
          </TableCell>
        </TableRow>)}</TableBody>
      </Table>}
    </CardContent>
  </Card>
}

export function BalanceCalculationSheet({ details, close }: { details: BalanceCalculation | null; close: () => void }) {
  return <Sheet open={details !== null} onOpenChange={open => { if (!open) close() }}>
    <SheetContent className="overflow-y-auto sm:max-w-xl">
      <SheetHeader>
        <SheetTitle>Как рассчитан баланс</SheetTitle>
        <SheetDescription>Оценка на {rateTime(details?.data.evaluatedAt ?? null)}. Показаны котировки, использованные для этого расчёта.</SheetDescription>
      </SheetHeader>
      <div className="flex flex-col gap-6 py-6">
        <p className="text-sm text-muted-foreground">Общий баланс = доступные средства + платежи, ожидающие зачисления. Ожидающие платежи показаны до удержания комиссии. Оценка по курсу носит справочный характер; вывод выполняется в выбранном токене.</p>
        {details?.tokens.map(token => <section key={token.tokenId} className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2"><h3 className="font-medium">{token.tokenSymbol}</h3>{token.network && <Badge variant="outline">{token.network}</Badge>}</div>
          <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">Доступно</dt><dd className="text-right tabular-nums [overflow-wrap:anywhere]">{formatDecimal(token.availableHuman)}</dd>
            <dt className="text-muted-foreground">Ожидает зачисления</dt><dd className="text-right tabular-nums [overflow-wrap:anywhere]">{formatDecimal(token.pendingHuman)}</dd>
            <dt className="text-muted-foreground">Всего</dt><dd className="text-right tabular-nums [overflow-wrap:anywhere]">{formatDecimal(token.humanAmount)}</dd>
          </dl>
          <p className="text-sm">{rateReasonLabels[token.valuation.reason] ?? "Оценка недоступна"}</p>
          {token.valuation.status === "identity" && <p>1 {details.data.baseCurrency.code} = 1 {details.data.baseCurrency.code}</p>}
          {token.valuation.selectedQuote ? <QuoteFacts quote={token.valuation.selectedQuote} /> : token.valuation.lastKnownQuote && <>
            <p className="text-sm text-muted-foreground">Последний известный курс; в расчёте не применён.</p><QuoteFacts quote={token.valuation.lastKnownQuote} />
          </>}
          {token.valuation.freshUntil && <p className="text-sm text-muted-foreground">Актуален до {rateTime(token.valuation.freshUntil)}</p>}
        </section>)}
      </div>
    </SheetContent>
  </Sheet>
}

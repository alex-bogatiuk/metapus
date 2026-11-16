"use client"

import { useState } from "react"
import Link from "next/link"
import useSWR from "swr"
import { ArrowRight } from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge, type BadgeProps } from "@/components/ui/badge"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { api } from "@/lib/api"
import { formatPortalAmount } from "@/configuration/crypto/lib/portal-overview"
import type { PortalInvoiceListResponse, PortalWithdrawalListResponse } from "@/configuration/crypto/types/portal-api"
import { OverviewEmpty, OverviewError, OverviewLoading } from "./overview-states"

const statuses: Record<string, { label: string; variant: BadgeProps["variant"] }> = {
  created: { label: "Создан", variant: "outline" },
  partially_paid: { label: "Оплачен частично", variant: "warning" },
  paid: { label: "Ожидает подтверждения", variant: "secondary" },
  confirmed: { label: "Подтверждён", variant: "success" },
  completed: { label: "Завершён", variant: "success" },
  underpaid: { label: "Недоплата", variant: "warning" },
  expired: { label: "Срок истёк", variant: "outline" },
  cancelled: { label: "Отменён", variant: "outline" },
  reversed: { label: "Отменён после подтверждения", variant: "destructive" },
  draft: { label: "Черновик", variant: "outline" },
  pending: { label: "В ожидании", variant: "secondary" },
  processing: { label: "Обрабатывается", variant: "secondary" },
  signed: { label: "Подписан", variant: "secondary" },
  broadcast: { label: "Отправлен в сеть", variant: "secondary" },
  failed: { label: "Ошибка вывода", variant: "destructive" },
}

export function RecentOperationsCard({ merchantId }: { merchantId: string | null }) {
  const [kind, setKind] = useState("invoices")
  const { data, error, isLoading, mutate } = useSWR<PortalInvoiceListResponse | PortalWithdrawalListResponse>(
    merchantId ? ["portal-recent-operations", merchantId, kind] : null,
    () => kind === "invoices"
      ? api.portal.invoices({ merchantId: merchantId!, limit: 5, sort: "created_at", order: "desc" })
      : api.portal.withdrawals({ merchantId: merchantId!, limit: 5, sort: "created_at", order: "desc" }),
    { keepPreviousData: false, refreshInterval: 60000 },
  )
  const listHref = kind === "invoices" ? "/portal/invoices" : "/portal/withdrawals"
  return <Card>
    <CardHeader>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-2"><CardTitle>Последние операции</CardTitle><CardDescription>Пять последних {kind === "invoices" ? "инвойсов" : "выводов"}</CardDescription></div>
        <div className="flex flex-wrap items-center gap-3">
          <ToggleGroup type="single" value={kind} onValueChange={value => { if (value) setKind(value) }} variant="outline" size="sm" aria-label="Тип операций">
            <ToggleGroupItem value="invoices">Инвойсы</ToggleGroupItem><ToggleGroupItem value="withdrawals">Выводы</ToggleGroupItem>
          </ToggleGroup>
          <Button variant="ghost" size="sm" asChild><Link href={listHref}>Все операции<ArrowRight data-icon="inline-end" /></Link></Button>
        </div>
      </div>
    </CardHeader>
    <CardContent>
      {isLoading ? <OverviewLoading label="Загрузка последних операций" /> : error ? <OverviewError title="Не удалось загрузить последние операции" retry={() => mutate().catch(() => undefined)} /> : !data?.items.length ? <OverviewEmpty title={kind === "invoices" ? "Инвойсов пока нет" : "Выводов пока нет"} description={kind === "invoices" ? "Создайте инвойс, чтобы принять платёж." : "Здесь появятся операции вывода средств."} /> : <Table>
        <TableHeader><TableRow><TableHead>Операция</TableHead><TableHead>Статус</TableHead><TableHead>Дата</TableHead><TableHead className="text-right">{kind === "invoices" ? "Получено / к оплате" : "Сумма"}</TableHead></TableRow></TableHeader>
        <TableBody>{data.items.map(item => {
          const status = statuses[item.status] ?? { label: "Неизвестный статус", variant: "outline" as const }
          return <TableRow key={item.id}>
            <TableCell><Link className="font-medium underline-offset-4 hover:underline" href={kind === "invoices" ? "/portal/invoices/" + item.id : listHref}>#{item.number}</Link><p className="mt-1 text-xs text-muted-foreground">{item.symbol} · {item.network}</p></TableCell>
            <TableCell><Badge variant={status.variant}>{status.label}</Badge></TableCell>
            <TableCell className="whitespace-nowrap text-muted-foreground">{new Date(item.createdAt).toLocaleString("ru-RU", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</TableCell>
            <TableCell className="text-right tabular-nums">
              <p className="font-medium">{formatPortalAmount("receivedAmount" in item ? item.receivedAmount : item.amount, item.decimalPlaces)} {item.symbol}</p>
              {"receivedAmount" in item && <p className="mt-1 text-xs text-muted-foreground">из {formatPortalAmount(item.amount, item.decimalPlaces)} {item.symbol}</p>}
            </TableCell>
          </TableRow>
        })}</TableBody>
      </Table>}
    </CardContent>
  </Card>
}

"use client"
import Link from "next/link"
import { api } from "@/lib/api"
import { cryptoApi } from "@/configuration/crypto/lib/api"
import { useWidgetData } from "@/hooks/useWidgetData"
import { queueKinds, queueHref, queueItemLabels, queueStatusLabels, observedTime } from "@/configuration/crypto/lib/admin-dashboard"
import { formatPortalAmount } from "@/configuration/crypto/lib/portal-overview"
import type { WidgetRenderProps } from "@/types/dashboard"
import type { QueueConfig } from "@/configuration/crypto/lib/widget-configs"
import { CircleCheck } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from "@/components/ui/table"
import { WidgetBody, WidgetEmpty, WidgetLink } from "@/components/dashboard/widgets/widget-state"
import { useCompactRows } from "@/components/dashboard/widgets/compact-rows"

type Kind = "withdrawal" | "inbound" | "outflow"
const listLabels: Record<Kind, string> = { withdrawal: "Все заявки", inbound: "Все поступления", outflow: "Все списания" }

export default function Queue({ config, isEditMode }: WidgetRenderProps<QueueConfig>) {
  const state = useWidgetData(() => cryptoApi.dashboard.queues(config.kind, config.limit), { key: `widget:admin-queue:${config.kind}:${config.limit}`, isEditMode, pollInterval: 60_000 })
  const data = state.data
  const counts: Record<Kind, number> = { withdrawal: data?.withdrawals ?? 0, inbound: data?.inbound ?? 0, outflow: data?.outflows ?? 0 }
  const total = config.kind === "all" ? data?.total : data ? counts[config.kind] : undefined
  // "All queues" has no list of its own: link the only busy queue, or name each busy queue in the footer.
  const busy = (Object.keys(counts) as Kind[]).filter(kind => counts[kind] > 0)
  const target: Kind | null = config.kind !== "all" ? config.kind : busy.length === 1 ? busy[0] : null
  const items = data?.items ?? []
  // Nothing waits for a decision: one row is enough to say so.
  const calm = !!data && !state.error && items.length === 0
  useCompactRows(calm ? 1 : null)
  const oldest = items.reduce<string | null>((min, item) => !min || Date.parse(item.createdAt) < Date.parse(min) ? item.createdAt : min, null)

  return <WidgetBody
    title="Требуют решения"
    description={`${queueKinds[config.kind]} · ${total == null ? "Загрузка" : `Всего: ${total}`}`}
    loading={state.loading} error={state.error} retry={state.refetch}
    action={target && !isEditMode ? <WidgetLink href={queueHref(target)}>{listLabels[target]}</WidgetLink> : undefined}
    flush={!!items.length}
    footer={!!total && <>
      <span>Показано {items.length} из {total}</span>
      {target || isEditMode
        ? oldest && <span>Самая ранняя: {observedTime(oldest)}</span>
        : <span className="flex flex-wrap gap-x-3">{busy.map(kind => <Link key={kind} href={queueHref(kind)} className="font-medium text-info hover:underline">{queueKinds[kind]}: {counts[kind]}</Link>)}</span>}
    </>}
  >
    {!items.length ? <WidgetEmpty inline tone="success" icon={CircleCheck} title="Очередь пуста" description="Операций для ручной проверки нет." /> : <Table>
      <TableHeader><TableRow><TableHead>Операция</TableHead><TableHead>Мерчант / сеть</TableHead><TableHead className="text-right">Сумма</TableHead></TableRow></TableHeader>
      <TableBody>{items.map(item => <TableRow key={item.id}>
        <TableCell>
          <div className="flex flex-wrap items-center gap-2">
            <Link href={queueHref(item.kind, item.id)} className="font-mono text-xs font-medium underline-offset-4 hover:underline">{item.number}</Link>
            <Badge variant="secondary">{queueItemLabels[item.kind as keyof typeof queueItemLabels] ?? "Проверка"}</Badge>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">{queueStatusLabels[item.status] ?? item.status} · {observedTime(item.createdAt)}</p>
        </TableCell>
        <TableCell>{item.merchant || "—"}<div className="mt-0.5 text-xs text-muted-foreground">{item.network}</div></TableCell>
        <TableCell className="whitespace-nowrap text-right tabular-nums">{item.symbol
          ? <><span className="font-medium">{formatPortalAmount(item.amount, item.decimalPlaces)}</span> <span className="text-muted-foreground">{item.symbol}</span></>
          : <span className="text-muted-foreground">Токен не определён</span>}</TableCell>
      </TableRow>)}</TableBody>
    </Table>}
  </WidgetBody>
}

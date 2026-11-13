"use client"
import Link from "next/link"
import { api } from "@/lib/api"
import { useWidgetData } from "@/hooks/useWidgetData"
import { queueKinds, queueHref, queueItemLabels, queueStatusLabels, observedTime } from "@/lib/admin-dashboard"
import { formatPortalAmount } from "@/lib/portal-overview"
import type { WidgetRenderProps } from "@/types/dashboard"
import { CircleCheck } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from "@/components/ui/table"
import { WidgetBody, WidgetEmpty } from "./widget-state"

export default function Queue({ config, isEditMode }: WidgetRenderProps<"attention-queue">) {
  const state = useWidgetData(() => api.dashboard.queues(config.kind, config.limit), { key: `widget:admin-queue:${config.kind}:${config.limit}`, isEditMode, pollInterval: 60_000 })
  const total = config.kind === "all" ? state.data?.total : config.kind === "withdrawal" ? state.data?.withdrawals : config.kind === "inbound" ? state.data?.inbound : state.data?.outflows
  return <WidgetBody title="Требуют решения" description={`${queueKinds[config.kind]} · ${total == null ? "Загрузка" : `Всего: ${total}`}`} loading={state.loading} error={state.error} retry={state.refetch}>
    {!state.data?.items.length ? <WidgetEmpty icon={CircleCheck} title="Очередь пуста" description="Операций для ручной проверки нет." /> : <Table><TableHeader><TableRow><TableHead>Операция</TableHead><TableHead>Мерчант / сеть</TableHead><TableHead className="text-right">Сумма</TableHead></TableRow></TableHeader><TableBody>{state.data.items.map(item => <TableRow key={item.id}><TableCell><div className="flex flex-wrap items-center gap-2"><Link href={queueHref(item.kind, item.id)} className="font-medium underline-offset-4 hover:underline">{item.number}</Link><Badge variant="secondary" className="px-1.5 py-0 font-medium">{queueItemLabels[item.kind as keyof typeof queueItemLabels] ?? "Проверка"}</Badge></div><p className="mt-0.5 text-xs text-muted-foreground">{queueStatusLabels[item.status] ?? item.status} · {observedTime(item.createdAt)}</p></TableCell><TableCell>{item.merchant || "—"}<div className="text-xs text-muted-foreground">{item.network}</div></TableCell><TableCell className="text-right tabular-nums">{item.symbol ? `${formatPortalAmount(item.amount, item.decimalPlaces)} ${item.symbol}` : "Токен не определён"}</TableCell></TableRow>)}</TableBody></Table>}
    {!!total && <p className="mt-3 text-xs text-muted-foreground">Показано {state.data?.items.length ?? 0} из {total}. Откройте операцию для проверки.</p>}
  </WidgetBody>
}

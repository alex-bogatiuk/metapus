"use client"

import { useState } from "react"
import Link from "next/link"
import useSWR from "swr"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { usePermissions } from "@/hooks/use-permissions"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import type { InboundReviewItem, InboundReviewOutcome, InboundReviewStatus } from "@/types/inbound-review"

const outcomes: Record<InboundReviewOutcome, string> = {
  credited_manually: "Зачислено вручную",
  refunded_manually: "Возвращено вручную",
  not_attributable: "Принадлежность не установлена",
}

export default function InboundReviewsPage() {
  const permissions = usePermissions()
  const canRead = permissions.has("crypto_inbound_review:read")
  const canResolve = permissions.has("crypto_inbound_review:resolve")
  const [status, setStatus] = useState<InboundReviewStatus>("needs_review")
  const [offset, setOffset] = useState(0)
  const [selected, setSelected] = useState<InboundReviewItem | null>(null)
  const [outcome, setOutcome] = useState<InboundReviewOutcome>("credited_manually")
  const [reference, setReference] = useState("")
  const [note, setNote] = useState("")
  const [saving, setSaving] = useState(false)
  const { data, error, isLoading, mutate } = useSWR(
    canRead ? ["inbound-reviews", status, offset] : null,
    () => api.inboundReviews.list(status, offset),
  )

  const resolve = async () => {
    if (!selected || !note.trim() || (outcome !== "not_attributable" && !reference.trim())) return
    setSaving(true)
    try {
      await api.inboundReviews.resolve(selected.id, outcome, reference.trim(), note.trim())
      toast.success("Решение сохранено")
      setSelected(null)
      await mutate()
    } catch (cause) {
      toast.error(cause instanceof Error ? cause.message : "Не удалось сохранить решение")
    } finally {
      setSaving(false)
    }
  }

  if (!canRead) return <p className="p-6 text-muted-foreground">Нет доступа к спорным входящим переводам.</p>

  return <div className="space-y-6 p-6">
    <div><h1 className="text-2xl font-semibold">Спорные входящие переводы</h1>
      <p className="text-sm text-muted-foreground">Дополнительные переводы по инвойсам «1 инвойс — 1 платёж» не зачисляются автоматически.</p></div>
    <Card><CardHeader><CardTitle>Очередь проверки</CardTitle>
      <CardDescription>Решение фиксирует результат отдельной процедуры и само не создаёт проводок или возврата.</CardDescription></CardHeader>
      <CardContent className="space-y-4">
        <div className="flex gap-2">
          <Button variant={status === "needs_review" ? "default" : "outline"} onClick={() => { setStatus("needs_review"); setOffset(0) }}>Ожидают решения</Button>
          <Button variant={status === "resolved" ? "default" : "outline"} onClick={() => { setStatus("resolved"); setOffset(0) }}>Разобраны</Button>
          <Button variant="outline" onClick={() => mutate()}>Обновить</Button>
        </div>
        {error && <p role="alert" className="text-sm text-destructive">Не удалось загрузить переводы.</p>}
        {isLoading ? <p className="text-sm text-muted-foreground">Загрузка…</p> : <Table>
          <TableHeader><TableRow><TableHead>Обнаружен</TableHead><TableHead>Инвойс</TableHead><TableHead>Сумма</TableHead><TableHead>Транзакция</TableHead><TableHead>Статус</TableHead><TableHead>Решение</TableHead></TableRow></TableHeader>
          <TableBody>{(data?.items ?? []).map((item) => <TableRow key={item.id}>
            <TableCell>{formatDateTime(item.createdAt)}</TableCell>
            <TableCell>{item.invoiceId ? <Link className="text-primary hover:underline" href={`/documents/crypto-invoice/${item.invoiceId}`}>{item.invoiceId.slice(0, 8)}</Link> : <div className="flex flex-col gap-1"><span>Выведенный адрес</span><span className="text-xs text-muted-foreground">{item.customerId}</span><span className="font-mono text-xs" title={item.toAddress}>{item.toAddress.slice(0,12)}…</span></div>}</TableCell>
            <TableCell className="font-mono">{item.amount}</TableCell>
            <TableCell className="font-mono" title={item.txHash}>{item.txHash.slice(0, 12)}…</TableCell>
            <TableCell>{item.matchReason === "retired_deposit_address" ? "Поступление на выведенный адрес" : item.invoiceStatus}</TableCell>
            <TableCell>{item.status === "resolved" ? <div className="space-y-1">
              <div>{outcomes[item.reviewOutcome as InboundReviewOutcome]}</div>
              <div className="text-xs text-muted-foreground" title={item.reviewedBy}>{item.reviewedAt ? formatDateTime(item.reviewedAt) : ""} · {item.reviewedBy.slice(0, 8)}</div>
              <div className="text-xs text-muted-foreground">{item.reviewNote}</div>
              {item.reviewReference && <a className="text-xs text-primary underline" href={item.reviewReference} target="_blank" rel="noreferrer">Внешняя операция</a>}
            </div> : canResolve && item.canResolve ? <Button size="sm" onClick={() => { setSelected(item); setOutcome("credited_manually"); setReference(""); setNote("") }}>Разобрать</Button> : "Ожидает закрытия инвойса"}</TableCell>
          </TableRow>)}
            {data?.items.length === 0 && <TableRow><TableCell colSpan={6} className="text-center text-muted-foreground">Переводов нет</TableCell></TableRow>}
          </TableBody></Table>}
        <div className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
          <span>{data?.total ?? 0} всего</span>
          <Button variant="outline" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 50))}>Назад</Button>
          <Button variant="outline" size="sm" disabled={offset + 50 >= (data?.total ?? 0)} onClick={() => setOffset(offset + 50)}>Далее</Button>
        </div>
      </CardContent></Card>
    <Dialog open={selected !== null} onOpenChange={(open) => { if (!open) setSelected(null) }}>
      <DialogContent><DialogHeader><DialogTitle>Зафиксировать решение</DialogTitle>
        <DialogDescription>Выполните зачисление или возврат отдельным процессом, затем укажите его ссылку.</DialogDescription></DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2"><Label>Результат</Label><Select value={outcome} onValueChange={(value) => setOutcome(value as InboundReviewOutcome)}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{Object.entries(outcomes).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}</SelectContent></Select></div>
          <div className="space-y-2"><Label htmlFor="review-reference">Ссылка на операцию {outcome === "not_attributable" ? "(необязательно)" : "(обязательно)"}</Label><Input id="review-reference" value={reference} onChange={(event) => setReference(event.target.value)} maxLength={500} /></div>
          <div className="space-y-2"><Label htmlFor="review-note">Обоснование</Label><Textarea id="review-note" value={note} onChange={(event) => setNote(event.target.value)} maxLength={2000} /></div>
        </div>
        <DialogFooter><Button disabled={saving || !note.trim() || (outcome !== "not_attributable" && !reference.trim())} onClick={resolve}>{saving ? "Сохранение…" : "Сохранить решение"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
}

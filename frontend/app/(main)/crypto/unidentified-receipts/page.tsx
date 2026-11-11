"use client"

import { useRef, useState } from "react"
import Link from "next/link"
import useSWR from "swr"
import { toast } from "sonner"
import { api, ApiError, idempotencyHeaders, shouldRetainMutationKey } from "@/lib/api"
import { formatDateTime, formatMinorUnits } from "@/lib/format"
import { usePermissions } from "@/hooks/use-permissions"
import { useAuthStore } from "@/stores/useAuthStore"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { ReferenceField } from "@/components/shared/reference-field"
import type { MerchantCreditResponse, UnidentifiedReceiptItem } from "@/types/generated/http-contracts"
import { receiptJournalStates, receiptReasons, receiptRowStateHints, receiptRowStates, type ReceiptJournalState } from "@/types/unidentified-receipt"

type Dialogs =
  | { kind: "credit"; item: UnidentifiedReceiptItem; merchantId: string; merchantName: string }
  | { kind: "reject"; item: UnidentifiedReceiptItem; reason: string }
  | null

const pageSize = 50

function amount(item: UnidentifiedReceiptItem, raw: string) {
  return `${formatMinorUnits(raw, item.decimalPlaces)} ${item.tokenSymbol}`
}

function short(value: string) {
  return value.length > 14 ? `${value.slice(0, 8)}…${value.slice(-4)}` : value
}

export default function UnidentifiedReceiptsPage() {
  const permissions = usePermissions()
  const user = useAuthStore((s) => s.user)
  const canRead = permissions.has("crypto_receipt:read")
  const canCredit = permissions.has("crypto_receipt:credit")
  const canApprove = permissions.has("crypto_receipt:approve")
  const [state, setState] = useState<ReceiptJournalState>("open")
  const [includeDust, setIncludeDust] = useState(false)
  const [offset, setOffset] = useState(0)
  const [dialog, setDialog] = useState<Dialogs>(null)
  const [busy, setBusy] = useState(false)
  // One key per opened command: a retry after a network error replays it.
  const commandKey = useRef("")
  const { data, error, isLoading, mutate } = useSWR(
    canRead ? ["unidentified-receipts", state, includeDust, offset] : null,
    () => api.unidentifiedReceipts.list(state, includeDust, offset),
  )

  const run = async (action: () => Promise<MerchantCreditResponse>, done: string) => {
    if (busy) return
    if (!commandKey.current) commandKey.current = idempotencyHeaders()["Idempotency-Key"]
    setBusy(true)
    try {
      await action()
      toast.success(done)
      commandKey.current = ""
      setDialog(null)
      await mutate()
    } catch (cause) {
      if (!shouldRetainMutationKey(cause)) commandKey.current = ""
      toast.error(cause instanceof ApiError || cause instanceof Error ? cause.message : "Не удалось выполнить действие")
    } finally {
      setBusy(false)
    }
  }

  const openDialog = (next: Dialogs) => {
    commandKey.current = ""
    setDialog(next)
  }

  const approve = (item: UnidentifiedReceiptItem) => {
    if (!item.credit) return
    commandKey.current = ""
    void run(() => api.unidentifiedReceipts.approveCredit(item.credit!.id, item.credit!.version, commandKey.current), "Зачисление подтверждено")
  }

  if (!canRead) return <p className="p-6 text-muted-foreground">Нет доступа к журналу невыясненных поступлений.</p>

  const items = data?.items ?? []
  return <div className="flex flex-col gap-6 p-6">
    <div className="flex flex-col gap-1">
      <h1 className="text-2xl font-semibold">Невыясненные поступления</h1>
      <p className="text-sm text-muted-foreground">
        Подтверждённые переводы поддерживаемых токенов, которые не удалось автоматически сопоставить со счётом.
        Они учтены на кошельке, но не зачислены мерчанту и не расходуются. Неизвестные токены и спам в журнал не попадают.
      </p>
    </div>
    <Card>
      <CardHeader>
        <CardTitle>Журнал</CardTitle>
        <CardDescription>Зачисление переводит весь остаток мерчанту по его тарифу. Выше порога токена нужно подтверждение другим сотрудником.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-2">
          {(Object.keys(receiptJournalStates) as ReceiptJournalState[]).map((value) =>
            <Button key={value} variant={state === value ? "default" : "outline"} onClick={() => { setState(value); setOffset(0) }}>
              {receiptJournalStates[value]}
            </Button>)}
          <Button variant="outline" onClick={() => mutate()}>Обновить</Button>
          <label className="ml-auto flex items-center gap-2 text-sm">
            <Switch checked={includeDust} onCheckedChange={(checked) => { setIncludeDust(checked); setOffset(0) }} />
            Показывать dust
          </label>
        </div>
        {error && <p role="alert" className="text-sm text-destructive">Не удалось загрузить журнал.</p>}
        {isLoading ? <p className="text-sm text-muted-foreground">Загрузка…</p> : <Table>
          <TableHeader><TableRow>
            <TableHead>Получено</TableHead><TableHead>Сеть / кошелёк</TableHead><TableHead>Отправитель</TableHead>
            <TableHead>Сумма</TableHead><TableHead>Причина</TableHead><TableHead>Состояние</TableHead><TableHead className="text-right">Действия</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {items.map((item) => {
              const credit = item.credit
              const ownCredit = !!credit && credit.preparedBy === user?.id
              const awaiting = item.state === "awaiting_approval" && !!credit
              return <TableRow key={item.receiptId || item.inboxEventId}>
                <TableCell>
                  <div>{formatDateTime(item.receivedAt)}</div>
                  {item.number && item.receiptId && <div><Link className="text-xs text-primary hover:underline"
                    href={`/documents/crypto-custody-receipt/${item.receiptId}`}>{item.number}</Link></div>}
                </TableCell>
                <TableCell>
                  <div>{item.networkName}</div>
                  <div className="font-mono text-xs text-muted-foreground" title={item.toAddress}>{short(item.toAddress)}</div>
                </TableCell>
                <TableCell>
                  <div className="font-mono text-xs" title={item.fromAddress}>{short(item.fromAddress)}</div>
                  <div className="font-mono text-xs text-muted-foreground" title={item.txHash}>{short(item.txHash)}</div>
                </TableCell>
                <TableCell className="font-mono">
                  <div>{amount(item, item.amount)}</div>
                  {item.remainder !== item.amount && <div className="text-xs text-muted-foreground">остаток {amount(item, item.remainder)}</div>}
                </TableCell>
                <TableCell>{item.reason === "dust" ? <Badge variant="outline">{receiptReasons.dust}</Badge> : receiptReasons[item.reason] ?? item.reason}</TableCell>
                <TableCell>
                  <div>{receiptRowStates[item.state] ?? item.state}</div>
                  {item.paymentId && <div><Link className="text-xs text-primary hover:underline" href={`/documents/crypto-payment/${item.paymentId}`}>
                    Платёж {item.paymentNumber}</Link></div>}
                  {receiptRowStateHints[item.state] && <div className="max-w-xs text-xs text-muted-foreground">{receiptRowStateHints[item.state]}</div>}
                  {credit && <div className="text-xs text-muted-foreground">
                    {credit.number} · {credit.merchantName} · {amount(item, credit.netAmount)} (комиссия {amount(item, credit.processingFee)})
                  </div>}
                  {credit?.status === "rejected" && <div className="text-xs text-muted-foreground">Отклонено: {credit.decisionReason}</div>}
                  {credit?.status === "reversed" && <div className="text-xs text-muted-foreground">Зачисление сторнировано: перевод исчез из сети</div>}
                  {item.pendingError && <div className="text-xs text-muted-foreground">{item.pendingError}</div>}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-2">
                    {item.state === "unattributed" && canCredit &&
                      <Button size="sm" onClick={() => openDialog({ kind: "credit", item, merchantId: item.custodyMerchantId, merchantName: "" })}>Зачислить мерчанту</Button>}
                    {awaiting && canApprove && !ownCredit &&
                      <Button size="sm" disabled={busy} onClick={() => approve(item)}>Подтвердить</Button>}
                    {awaiting && ownCredit && <span className="text-xs text-muted-foreground">Ждёт другого сотрудника</span>}
                    {awaiting && canCredit &&
                      <Button size="sm" variant="outline" onClick={() => openDialog({ kind: "reject", item, reason: "" })}>Отклонить</Button>}
                  </div>
                </TableCell>
              </TableRow>
            })}
            {items.length === 0 && <TableRow><TableCell colSpan={7} className="text-center text-muted-foreground">Поступлений нет</TableCell></TableRow>}
          </TableBody>
        </Table>}
        <div className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
          <span>{data?.total ?? 0} всего</span>
          <Button variant="outline" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>Назад</Button>
          <Button variant="outline" size="sm" disabled={offset + pageSize >= (data?.total ?? 0)} onClick={() => setOffset(offset + pageSize)}>Далее</Button>
        </div>
      </CardContent>
    </Card>

    <Dialog open={dialog !== null} onOpenChange={(open) => { if (!open && !busy) setDialog(null) }}>
      <DialogContent>
        {dialog?.kind === "credit" && <>
          <DialogHeader>
            <DialogTitle>Зачислить мерчанту</DialogTitle>
            <DialogDescription>
              Будет зачислен весь остаток {amount(dialog.item, dialog.item.remainder)} по тарифу мерчанта на текущий момент.
              Деньги на кошельке не двигаются: меняется только их принадлежность.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="credit-merchant">Мерчант</FieldLabel>
              <ReferenceField id="credit-merchant" value={dialog.merchantId} displayName={dialog.merchantName}
                apiEndpoint="/catalog/merchants" disabled={!!dialog.item.custodyMerchantId}
                onChange={(merchantId, merchantName) => setDialog({ ...dialog, merchantId, merchantName })} />
              {dialog.item.custodyMerchantId && <FieldDescription>Кошелёк принадлежит мерчанту, зачислить можно только ему.</FieldDescription>}
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button disabled={busy || !dialog.merchantId}
              onClick={() => run(() => api.unidentifiedReceipts.prepareCredit(dialog.item.receiptId, dialog.merchantId, commandKey.current), "Зачисление создано")}>
              {busy ? "Сохранение…" : "Зачислить"}
            </Button>
          </DialogFooter>
        </>}
        {dialog?.kind === "reject" && dialog.item.credit && <>
          <DialogHeader>
            <DialogTitle>Отклонить зачисление {dialog.item.credit.number}</DialogTitle>
            <DialogDescription>Поступление останется в невыясненных, его можно будет зачислить заново.</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="reject-reason">Причина</FieldLabel>
              <Textarea id="reject-reason" value={dialog.reason} maxLength={2000}
                onChange={(event) => setDialog({ ...dialog, reason: event.target.value })} />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button variant="destructive" disabled={busy || !dialog.reason.trim()}
              onClick={() => run(() => api.unidentifiedReceipts.rejectCredit(dialog.item.credit!.id, dialog.item.credit!.version, dialog.reason.trim(), commandKey.current), "Зачисление отклонено")}>
              {busy ? "Сохранение…" : "Отклонить"}
            </Button>
          </DialogFooter>
        </>}
      </DialogContent>
    </Dialog>
  </div>
}

"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { Copy, ExternalLink } from "lucide-react"
import { useRouter } from "next/navigation"
import { toast } from "sonner"

import { formatRiskClass } from "@/configuration/crypto/lib/aml-risk"
import { usePermissions } from "@/hooks/use-permissions"
import { buildEntityUrlByRoute } from "@/lib/entity-url"
import { copyToClipboard, formatDateTime } from "@/lib/format"
import { formatRawMinor } from "@/lib/money"
import type { AdminWithdrawalRequestDetail, WithdrawalRequestStatus } from "@/configuration/crypto/types/withdrawal-request"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent,
  AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"

import { cryptoApi } from "@/configuration/crypto/lib/api"
interface WithdrawalRequestSelectionPanelProps {
  item: { id: string } | null
  refresh: () => void
}

const statusLabels: Record<WithdrawalRequestStatus, string> = {
  pending_signature: "Ожидает подписи",
  pending_approval: "Ожидает решения",
  approved: "Утверждена",
  signing: "Подписывается",
  broadcast: "Отправлена",
  confirmed: "Исполнена",
  held: "Требует сверки",
  rejected: "Отклонена",
  failed: "Ошибка",
  partially_completed: "Частично исполнена",
}

export function WithdrawalRequestSelectionPanel({ item, refresh }: WithdrawalRequestSelectionPanelProps) {
	const [sourcesOpen,setSourcesOpen] = useState(false)
  const router = useRouter()
  const { has } = usePermissions()
  const selectedID = item?.id ?? null
  const selectedIDRef = useRef(selectedID)
  selectedIDRef.current = selectedID
  const [detail, setDetail] = useState<AdminWithdrawalRequestDetail | null>(null)
  const [signingJob, setSigningJob] = useState<{ status: string; attempts: number; lastError: string } | null>(null)
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [approveOpen, setApproveOpen] = useState(false)
  const [rejectOpen, setRejectOpen] = useState(false)
  const [reason, setReason] = useState("")

  const loadDetail = useCallback(async (id: string) => {
    const next = await cryptoApi.withdrawalRequests.get(id)
    if (selectedIDRef.current !== id) return
    setDetail(next)
    if (next.status === "pending_signature") {
      const job = await cryptoApi.withdrawalRequests.signingJob(id).catch(() => null)
      if (selectedIDRef.current === id) setSigningJob(job)
    } else {
      setSigningJob(null)
    }
  }, [])

  useEffect(() => {
    if (!selectedID) {
      setDetail(null)
      setSigningJob(null)
      return
    }
    let cancelled = false
    setDetail(null)
    setSigningJob(null)
    setLoading(true)
    loadDetail(selectedID)
      .catch(() => { if (!cancelled) toast.error("Не удалось загрузить заявку") })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [selectedID, loadDetail])

  const retrySigning = async () => {
    if (!detail || busy || signingJob?.status !== "needs_review") return
    setBusy(true)
    try {
      await cryptoApi.withdrawalRequests.retrySigning(detail.id)
      toast.success("Задание подписи снова в очереди")
      await loadDetail(detail.id)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось повторить подписание")
    } finally {
      setBusy(false)
    }
  }

  const runAction = async (action: "approve" | "reject") => {
    if (!detail || busy || (detail.status !== "pending_approval" && (action !== "reject" || detail.status !== "pending_signature"))) return
    setBusy(true)
    try {
      if (action === "approve") {
        await cryptoApi.withdrawalRequests.approve(detail.id)
        toast.success("Заявка утверждена")
      } else {
        await cryptoApi.withdrawalRequests.reject(detail.id, reason.trim())
        toast.success("Заявка отклонена; сумма возвращена на баланс")
        setReason("")
        setRejectOpen(false)
      }
      refresh()
      await loadDetail(detail.id)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Не удалось выполнить действие")
      await loadDetail(detail.id).catch(() => undefined)
    } finally {
      setBusy(false)
    }
  }

  if (!selectedID) {
    return <aside className="shrink-0 bg-card px-4 py-3 text-xs text-muted-foreground">Выберите заявку в списке, чтобы увидеть её реквизиты и действия.</aside>
  }

  if (loading || !detail || detail.id !== selectedID) {
    return <aside className="shrink-0 bg-card px-4 py-3 text-xs text-muted-foreground">Загрузка заявки…</aside>
  }

  const canApprove = detail.status === "pending_approval" && has("document:withdrawal_request:post")
  const canReject = (detail.status === "pending_signature" || detail.status === "pending_approval") && has("document:withdrawal_request:post")
  const amount = `${formatRawMinor(detail.amount, detail.decimalPlaces)} ${detail.symbol}`
  const valuation = detail.valuation
  const usdAmount = valuation?.amount != null && (valuation.status === "fresh" || valuation.status === "identity")
    ? `≈ ${valuation.amount} USD`
    : "Оценка USD недоступна"
  const reasonBytes = new TextEncoder().encode(reason.trim()).length
  // A refund returns a credited payment to its payer out of the merchant's balance.
  const refund = detail.basisType === "CryptoPayment"

  return (
    <aside aria-label="Выбранная заявка на вывод" className="shrink-0 bg-card px-4 py-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <strong className="text-sm">Заявка {detail.number}</strong>
            <Badge variant="outline">{statusLabels[detail.status] ?? detail.status}</Badge>
			{detail.riskClass && <Badge variant="outline">Класс: {formatRiskClass(detail.riskClass)}</Badge>}
            {refund && <Badge variant="secondary">Возврат платежа</Badge>}
            <span className="text-xs text-muted-foreground">{detail.merchantName}</span>
          </div>
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="font-mono text-sm font-semibold tabular-nums">{amount}</span>
            <span className="text-sm text-muted-foreground">{usdAmount}</span>
          </div>
          {valuation?.amount != null && valuation.rate && (
            <p className="text-xs text-muted-foreground">
              Оценка по курсу {valuation.rate} USD за {valuation.multiplier ?? 1} {detail.symbol}
              {valuation.source ? ` · ${valuation.source}` : ""}
              {valuation.rateAsOf ? ` · ${formatDateTime(valuation.rateAsOf)}` : ""}
            </p>
          )}
          {refund && detail.basisId && (
            <p className="text-xs text-muted-foreground">
              Возврат по <a className="text-primary hover:underline" href={buildEntityUrlByRoute("crypto-payment", "document", detail.basisId)}>платежу</a>
              {detail.description ? ` · ${detail.description}` : ""}
            </p>
          )}
          {detail.status === "rejected" && detail.rejectionReason && (
            <p className="text-xs text-muted-foreground">Причина отклонения: {detail.rejectionReason}</p>
          )}
          {signingJob?.status === "needs_review" && (
            <p className="text-xs text-destructive">Подпись требует проверки после {signingJob.attempts} попыток: {signingJob.lastError}</p>
          )}
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>{detail.network}</span>
            <span className="max-w-[28rem] truncate font-mono" title={detail.destAddress}>{detail.destAddress}</span>
            <Button variant="ghost" size="icon" aria-label="Скопировать адрес" onClick={() => {
              copyToClipboard(detail.destAddress)
              toast.success("Адрес скопирован")
            }}><Copy /></Button>
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
		  {detail.sources && detail.sources.length>0 && <Button variant="outline" size="sm" onClick={()=>setSourcesOpen(true)}>Источники ({detail.sources.length})</Button>}
          <Button variant="outline" size="sm" onClick={() => router.push(buildEntityUrlByRoute("withdrawal-request", "document", detail.id))}>
            <ExternalLink /> Открыть
          </Button>
          {canReject && <Button variant="outline" size="sm" disabled={busy} onClick={() => setRejectOpen(true)}>Отклонить</Button>}
          {canReject && signingJob?.status === "needs_review" && (
            <Button variant="outline" size="sm" disabled={busy} onClick={() => void retrySigning()}>Повторить подпись</Button>
          )}
          {canApprove && <Button size="sm" disabled={busy} onClick={() => setApproveOpen(true)}>Утвердить</Button>}
        </div>
      </div>

      <AlertDialog open={approveOpen} onOpenChange={setApproveOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Утвердить заявку {detail.number}?</AlertDialogTitle>
            <AlertDialogDescription>{refund ? "Возврат" : "Сумма"} {amount} будет передан{refund ? "" : "а"} на исполнение по адресу {detail.destAddress}.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Назад</AlertDialogCancel>
            <AlertDialogAction disabled={busy} onClick={() => void runAction("approve")}>Утвердить</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

	  <Dialog open={sourcesOpen} onOpenChange={setSourcesOpen}>
		<DialogContent className="max-w-3xl"><DialogHeader><DialogTitle>Источники заявки {detail.number}</DialogTitle></DialogHeader>
		<div className="flex max-h-[60vh] flex-col gap-4 overflow-auto">{detail.sources?.map((source)=><div key={source.id} className="flex flex-col gap-2">
			<p className="break-all font-mono">{source.address}</p>
			<p>{formatRawMinor(source.amount,detail.decimalPlaces)} {detail.symbol} · {source.status}</p>
			<p>При заявке: {formatRiskClass(source.requestedClass)}; при отправке: {source.dispatchedClass?formatRiskClass(source.dispatchedClass):"ещё не отправлено"}; сейчас: {formatRiskClass(source.currentClass)}</p>
			{source.txHash&&<p className="break-all font-mono">{source.txHash}</p>}
		</div>)}</div></DialogContent>
	  </Dialog>
      <Dialog open={rejectOpen} onOpenChange={setRejectOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Отклонить заявку {detail.number}?</DialogTitle>
            <DialogDescription>Зарезервированная сумма будет возвращена на баланс мерчанта. Укажите причину решения.</DialogDescription>
          </DialogHeader>
          <Textarea value={reason} maxLength={2000} onChange={(event) => setReason(event.target.value)} aria-label="Причина отклонения" />
          <p className="text-xs text-muted-foreground">{reasonBytes} / 2000 байт</p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRejectOpen(false)}>Назад</Button>
            <Button variant="destructive" disabled={busy || reasonBytes === 0 || reasonBytes > 2000} onClick={() => void runAction("reject")}>Отклонить заявку</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </aside>
  )
}

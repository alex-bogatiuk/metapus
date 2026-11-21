"use client"

import { useCallback, useEffect, useState } from "react"
import Link from "next/link"
import {
  CheckCircle2,
  Clock,
  Copy,
  History,
  Loader2,
  Plus,
  Radio,
  RefreshCw,
  Send,
  Shield,
  XCircle,
} from "lucide-react"
import { toast } from "sonner"

import { formatRiskClass } from "@/configuration/crypto/lib/aml-risk"
import { copyToClipboard, formatDateTime, formatMinorUnits } from "@/lib/format"
import { truncateAddress } from "@/configuration/crypto/lib/blockchain"
import { usePortalStore } from "@/configuration/crypto/stores/usePortalStore"
import type { PortalWithdrawalRequestItem } from "@/configuration/crypto/types/portal-api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"

import { cryptoApi } from "@/configuration/crypto/lib/api"
const REQUEST_STATUS: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline"; icon: typeof Clock }> = {
  pending_signature: { label: "Ожидает подписи", variant: "outline", icon: Clock },
  pending_approval: { label: "На одобрении", variant: "outline", icon: Clock },
  approved: { label: "Одобрена", variant: "secondary", icon: CheckCircle2 },
  signing: { label: "Подписание", variant: "secondary", icon: Send },
  broadcast: { label: "Отправлена", variant: "secondary", icon: Radio },
  confirmed: { label: "Подтверждена", variant: "default", icon: CheckCircle2 },
  rejected: { label: "Отклонена", variant: "destructive", icon: XCircle },
  partially_completed: {label:"Частично выполнена",variant:"outline",icon:CheckCircle2},
  held: {label:"Требует разбора",variant:"outline",icon:Clock},
  failed: { label: "Ошибка", variant: "destructive", icon: XCircle },
}

export default function PortalWithdrawalRequestsPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [sourceRequest,setSourceRequest] = useState<PortalWithdrawalRequestItem | null>(null)
  const [items, setItems] = useState<PortalWithdrawalRequestItem[]>([])
  const [loading, setLoading] = useState(true)
  const [rejectingId, setRejectingId] = useState<string | null>(null)
  const [rejectReason, setRejectReason] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const fetchData = useCallback(async () => {
    if (!activeMerchantId) {
      setItems([])
      setLoading(false)
      return
    }
    setLoading(true)
    try {
      const res = await cryptoApi.portal.withdrawalRequests(activeMerchantId)
      setItems(res.items)
    } catch {
      toast.error("Не удалось загрузить заявки")
      setItems([])
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const handleReject = async () => {
    if (!activeMerchantId || !rejectingId) return
    setSubmitting(true)
    try {
      await cryptoApi.portal.rejectWithdrawalRequest(activeMerchantId, rejectingId, rejectReason)
      toast.success("Неотправленные средства возвращены на баланс")
      setRejectingId(null)
      setRejectReason("")
      await fetchData()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Не удалось отклонить заявку")
    } finally {
      setSubmitting(false)
    }
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Shield className="mb-3 size-10 opacity-40" />
        <p>Выберите мерчанта</p>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Заявки на вывод</h1>
          <p className="mt-1 text-sm text-muted-foreground">Создание, одобрение и исполнение выводов</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={fetchData} disabled={loading}>
            <RefreshCw className={loading ? "mr-2 size-4 animate-spin" : "mr-2 size-4"} />
            Обновить
          </Button>
          <Button variant="outline" size="sm" asChild>
            <Link href="/portal/withdrawals">
              <History className="mr-2 size-4" />
              История выводов
            </Link>
          </Button>
          <Button variant="outline" size="sm" asChild>
            <Link href="/portal/withdrawals/addresses">
              <Shield className="mr-2 size-4" />
              Адреса
            </Link>
          </Button>
          <Button size="sm" asChild>
            <Link href="/portal/withdrawals/new">
              <Plus className="mr-2 size-4" />
              Новая заявка
            </Link>
          </Button>
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Номер</TableHead>
                <TableHead>Статус</TableHead>
                <TableHead>Сумма</TableHead>
                <TableHead>Сеть</TableHead>
                <TableHead>Адрес</TableHead>
                <TableHead>Дата</TableHead>
                <TableHead className="text-right">Действия</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 7 }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className="h-4 w-full" />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : items.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="h-32 text-center text-muted-foreground">
                    Нет заявок
                  </TableCell>
                </TableRow>
              ) : (
                items.map((item) => {
                  const status = REQUEST_STATUS[item.status] ?? REQUEST_STATUS.pending_approval
                  const StatusIcon = status.icon
                  return (
                    <TableRow key={item.id}>
                      <TableCell className="font-mono text-sm">{item.number}
                        {item.refundPaymentId && <div><Badge variant="secondary" title={item.refundReason}>Возврат платежа</Badge></div>}
                        {item.riskClass && <div className="flex flex-col gap-2"><Badge variant="outline">{formatRiskClass(item.riskClass)}</Badge>
                          <Button variant="ghost" size="sm" onClick={()=>setSourceRequest(item)}>Источники ({item.sources?.length??0})</Button></div>}
                      </TableCell>
                      <TableCell>
                        <Badge variant={status.variant} className="gap-1">
                          <StatusIcon className="size-3" />
                          {status.label}
                        </Badge>
                      </TableCell>
                      <TableCell className="font-mono tabular-nums">
                        {formatMinorUnits(item.amount, item.decimalPlaces)} {item.symbol}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">{item.network}</TableCell>
                      <TableCell>
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-auto px-1 py-0 font-mono text-xs"
                                onClick={() => {
                                  copyToClipboard(item.destAddress)
                                  toast.success("Адрес скопирован")
                                }}
                              >
                                {truncateAddress(item.destAddress)}
                                <Copy className="ml-1 size-3 opacity-50" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>
                              <p className="font-mono text-xs">{item.destAddress}</p>
                            </TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">{formatDateTime(item.createdAt)}</TableCell>
                      <TableCell className="text-right">
                        {(item.status === "pending_signature" || item.status === "pending_approval" || (item.riskClass && ["approved","signing","broadcast","held"].includes(item.status) && item.sources?.some((source)=>source.cancellable))) ? (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setRejectingId(item.id)
                              setRejectReason("")
                            }}
                          >
                            <XCircle className="mr-2 size-4" />
                            {item.riskClass?"Отменить неотправленное":"Отклонить"}
                          </Button>
                        ) : item.rejectionReason ? (
                          <span className="text-xs text-muted-foreground">{item.rejectionReason}</span>
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={!!rejectingId} onOpenChange={(open) => !open && setRejectingId(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Отклонить заявку</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="reject-reason">Причина</Label>
            <Textarea
              id="reject-reason"
              value={rejectReason}
              onChange={(event) => setRejectReason(event.target.value)}
              rows={3}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRejectingId(null)} disabled={submitting}>
              Отмена
            </Button>
            <Button variant="destructive" onClick={handleReject} disabled={submitting}>
              {submitting && <Loader2 className="mr-2 size-4 animate-spin" />}
              Отклонить
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={!!sourceRequest} onOpenChange={(open)=>{if(!open)setSourceRequest(null)}}>
        <DialogContent className="max-w-3xl">
          <DialogHeader><DialogTitle>Источники вывода {sourceRequest?.number}</DialogTitle></DialogHeader>
          <div className="flex max-h-[60vh] flex-col gap-4 overflow-auto">
            {sourceRequest?.sources?.map((source)=><Card key={source.id}><CardContent className="flex flex-col gap-2 pt-4">
              <p className="break-all font-mono">{source.address}</p>
              <p>{formatMinorUnits(source.amount,sourceRequest.decimalPlaces)} {sourceRequest.symbol} · {source.status}</p>
              <p>При заявке: {formatRiskClass(source.requestedClass)}; при отправке: {source.dispatchedClass?formatRiskClass(source.dispatchedClass):"ещё не отправлено"}; сейчас: {formatRiskClass(source.currentClass)}</p>
              {source.txHash && <p className="break-all font-mono">{source.txHash}</p>}
            </CardContent></Card>)}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}

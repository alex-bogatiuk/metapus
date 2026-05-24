"use client"

import { useCallback, useEffect, useState } from "react"
import { useParams, useRouter } from "next/navigation"
import {
  ArrowLeft,
  CheckCircle2,
  Clock,
  Copy,
  ExternalLink,
  Loader2,
  XCircle,
} from "lucide-react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { usePortalStore } from "@/stores/usePortalStore"
import { cn } from "@/lib/utils"
import { truncateAddress } from "@/lib/blockchain"
import { formatMinorUnits, formatDateTime, copyToClipboard } from "@/lib/format"
import type { PortalInvoiceDetailResponse } from "@/types/portal-api"

// ── Status Config ──────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline"; icon: typeof CheckCircle2; color: string }> = {
  created:        { label: "Создан",       variant: "outline",      icon: Clock,         color: "border-muted-foreground" },
  partially_paid: { label: "Частично",     variant: "secondary",    icon: Clock,         color: "border-yellow-500" },
  paid:           { label: "Оплачен",      variant: "default",      icon: CheckCircle2,  color: "border-blue-500" },
  confirmed:      { label: "Подтверждён",  variant: "default",      icon: CheckCircle2,  color: "border-green-500" },
  overpaid:       { label: "Переплата",    variant: "secondary",    icon: CheckCircle2,  color: "border-orange-500" },
  expired:        { label: "Истёк",        variant: "destructive",  icon: XCircle,       color: "border-red-500" },
  cancelled:      { label: "Отменён",      variant: "destructive",  icon: XCircle,       color: "border-red-500" },
}



// ── Timeline Event Labels ──────────────────────────────────────────────

const EVENT_LABELS: Record<string, string> = {
  payment_detected:   "Платёж обнаружен",
  confirmation_update: "Подтверждение",
  payment_confirmed:  "Платёж подтверждён",
  invoice_expired:    "Инвойс истёк",
  invoice_cancelled:  "Инвойс отменён",
  invoice_overpaid:   "Переплата",
  sweep_initiated:    "Sweep начат",
  sweep_confirmed:    "Sweep подтверждён",
}

// ── Page ────────────────────────────────────────────────────────────────

export default function InvoiceDetailPage() {
  const { id } = useParams<{ id: string }>()
  const router = useRouter()
  const { activeMerchantId } = usePortalStore()
  const [invoice, setInvoice] = useState<PortalInvoiceDetailResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const fetchData = useCallback(async () => {
    if (!id) return
    setLoading(true)
    setError(null)
    try {
      const res = await api.portal.invoiceDetail(id, activeMerchantId || undefined)
      setInvoice(res)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Не удалось загрузить инвойс")
    } finally {
      setLoading(false)
    }
  }, [id, activeMerchantId])

  useEffect(() => {
    fetchData()
  }, [fetchData])


  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-3">
          <Skeleton className="h-8 w-8" />
          <Skeleton className="h-8 w-48" />
        </div>
        <div className="grid gap-6 md:grid-cols-2">
          <Skeleton className="h-64" />
          <Skeleton className="h-64" />
        </div>
        <Skeleton className="h-48" />
      </div>
    )
  }

  if (error || !invoice) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" size="sm" onClick={() => router.back()}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Назад
        </Button>
        <Card>
          <CardContent className="flex items-center justify-center h-32 text-muted-foreground">
            {error || "Инвойс не найден"}
          </CardContent>
        </Card>
      </div>
    )
  }

  const statusCfg = STATUS_CONFIG[invoice.status] ?? STATUS_CONFIG.created
  const StatusIcon = statusCfg.icon

  return (
    <div className="space-y-6">
      {/* Back Button + Header */}
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" onClick={() => router.back()}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight">
            Инвойс {invoice.number}
          </h1>
          <Badge variant={statusCfg.variant} className="gap-1">
            <StatusIcon className="h-3 w-3" />
            {statusCfg.label}
          </Badge>
        </div>
      </div>

      {/* Main Info Grid */}
      <div className="grid gap-6 md:grid-cols-2">
        {/* Left: Amount & Details */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Детали инвойса</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-y-3 text-sm">
              <span className="text-muted-foreground">Ожидаемая сумма</span>
              <span className="font-mono tabular-nums font-medium">
                {formatMinorUnits(invoice.amount, invoice.decimalPlaces)} {invoice.symbol}
              </span>

              <span className="text-muted-foreground">Получено</span>
              <span className="font-mono tabular-nums font-medium">
                {formatMinorUnits(invoice.receivedAmount, invoice.decimalPlaces)} {invoice.symbol}
              </span>

              {invoice.processingFee && (
                <>
                  <span className="text-muted-foreground">Комиссия</span>
                  <span className="font-mono tabular-nums text-muted-foreground">
                    {formatMinorUnits(invoice.processingFee, invoice.decimalPlaces)} {invoice.symbol}
                  </span>
                </>
              )}

              {invoice.netAmount && (
                <>
                  <span className="text-muted-foreground">Нетто</span>
                  <span className="font-mono tabular-nums font-medium text-green-600 dark:text-green-400">
                    {formatMinorUnits(invoice.netAmount, invoice.decimalPlaces)} {invoice.symbol}
                  </span>
                </>
              )}

              <Separator className="col-span-2 my-1" />

              <span className="text-muted-foreground">Сеть</span>
              <span>{invoice.network || "—"}</span>

              <span className="text-muted-foreground">Адрес кошелька</span>
              <span className="flex items-center gap-1">
                <span className="font-mono text-xs">{truncateAddress(invoice.walletAddress || "—")}</span>
                {invoice.walletAddress && (
                  <Button variant="ghost" size="icon" className="h-5 w-5" onClick={() => copyToClipboard(invoice.walletAddress)}>
                    <Copy className="h-3 w-3" />
                  </Button>
                )}
              </span>

              <span className="text-muted-foreground">Создан</span>
              <span className="text-muted-foreground">{formatDateTime(invoice.createdAt)}</span>

              <span className="text-muted-foreground">Истекает</span>
              <span className="text-muted-foreground">{formatDateTime(invoice.expiresAt)}</span>

              {invoice.externalId && (
                <>
                  <span className="text-muted-foreground">External ID</span>
                  <span className="font-mono text-xs">{invoice.externalId}</span>
                </>
              )}

              {invoice.orderId && (
                <>
                  <span className="text-muted-foreground">Order ID</span>
                  <span className="font-mono text-xs">{invoice.orderId}</span>
                </>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Right: Payment Info */}
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Платёж</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {invoice.txHash ? (
              <div className="grid grid-cols-2 gap-y-3 text-sm">
                <span className="text-muted-foreground">TX Hash</span>
                <span className="flex items-center gap-1 font-mono text-xs">
                  {truncateAddress(invoice.txHash)}
                  <Button variant="ghost" size="icon" className="h-5 w-5" onClick={() => copyToClipboard(invoice.txHash!)}>
                    <Copy className="h-3 w-3" />
                  </Button>
                </span>

                {invoice.fromAddress && (
                  <>
                    <span className="text-muted-foreground">Отправитель</span>
                    <span className="flex items-center gap-1 font-mono text-xs">
                      {truncateAddress(invoice.fromAddress)}
                      <Button variant="ghost" size="icon" className="h-5 w-5" onClick={() => copyToClipboard(invoice.fromAddress!)}>
                        <Copy className="h-3 w-3" />
                      </Button>
                    </span>
                  </>
                )}

                {invoice.confirmedAt && (
                  <>
                    <span className="text-muted-foreground">Подтверждён</span>
                    <span className="text-muted-foreground">{formatDateTime(invoice.confirmedAt)}</span>
                  </>
                )}
              </div>
            ) : (
              <div className="flex items-center justify-center h-24 text-muted-foreground text-sm">
                Платёж ещё не получен
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Timeline */}
      {invoice.timeline.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Таймлайн</CardTitle>
            <CardDescription>Хронология событий инвойса</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="relative space-y-0">
              {invoice.timeline.map((event, idx) => {
                const isLast = idx === invoice.timeline.length - 1
                const label = EVENT_LABELS[event.eventType] || event.eventType

                return (
                  <div key={event.id} className="flex gap-4">
                    {/* Vertical line + dot */}
                    <div className="flex flex-col items-center">
                      <div className={cn(
                        "h-3 w-3 rounded-full border-2 mt-1.5",
                        event.toStatus === "confirmed" ? "border-green-500 bg-green-500/20" :
                        event.toStatus === "expired" || event.toStatus === "failed" ? "border-red-500 bg-red-500/20" :
                        "border-blue-500 bg-blue-500/20"
                      )} />
                      {!isLast && <div className="w-px flex-1 bg-border min-h-[24px]" />}
                    </div>

                    {/* Content */}
                    <div className="pb-4 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium">{label}</span>
                        <span className="text-xs text-muted-foreground">{formatDateTime(event.createdAt)}</span>
                      </div>
                      <div className="flex items-center gap-2 mt-0.5">
                        {event.fromStatus && event.toStatus && (
                          <span className="text-xs text-muted-foreground">
                            {event.fromStatus} → {event.toStatus}
                          </span>
                        )}
                        {event.metadata.confirmations !== undefined && event.metadata.requiredConfs !== undefined && (
                          <Badge variant="outline" className="text-xs h-5">
                            {event.metadata.confirmations}/{event.metadata.requiredConfs} подтверждений
                          </Badge>
                        )}
                        {event.metadata.txHash && (
                          <span className="font-mono text-xs text-muted-foreground">
                            tx: {event.metadata.txHash.slice(0, 10)}…
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Webhook Deliveries */}
      {invoice.webhookDeliveries.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Вебхук-доставка</CardTitle>
            <CardDescription>Уведомления, отправленные по этому инвойсу</CardDescription>
          </CardHeader>
          <CardContent className="p-0">
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="border-b">
                  <tr>
                    <th className="text-left font-medium p-3 text-muted-foreground">Событие</th>
                    <th className="text-left font-medium p-3 text-muted-foreground">Статус</th>
                    <th className="text-left font-medium p-3 text-muted-foreground">Время</th>
                    <th className="text-left font-medium p-3 text-muted-foreground">Попытка</th>
                    <th className="text-left font-medium p-3 text-muted-foreground">Дата</th>
                  </tr>
                </thead>
                <tbody>
                  {invoice.webhookDeliveries.map((d) => {
                    const isSuccess = d.statusCode !== null && d.statusCode >= 200 && d.statusCode < 300
                    return (
                      <tr key={d.id} className="border-b last:border-0 hover:bg-muted/50 transition-colors">
                        <td className="p-3">
                          <Badge variant="outline" className="font-mono text-xs">
                            {d.eventType}
                          </Badge>
                        </td>
                        <td className="p-3">
                          {isSuccess ? (
                            <Badge variant="default" className="gap-1">
                              <CheckCircle2 className="h-3 w-3" />
                              {d.statusCode}
                            </Badge>
                          ) : (
                            <TooltipProvider>
                              <Tooltip>
                                <TooltipTrigger>
                                  <Badge variant="destructive" className="gap-1">
                                    <XCircle className="h-3 w-3" />
                                    {d.statusCode ?? "ERR"}
                                  </Badge>
                                </TooltipTrigger>
                                {d.errorMessage && (
                                  <TooltipContent className="max-w-xs">
                                    <p className="text-xs">{d.errorMessage}</p>
                                  </TooltipContent>
                                )}
                              </Tooltip>
                            </TooltipProvider>
                          )}
                        </td>
                        <td className="p-3 tabular-nums">
                          {d.responseTimeMs !== null ? `${d.responseTimeMs}ms` : "—"}
                        </td>
                        <td className="p-3">#{d.attempt}</td>
                        <td className="p-3 text-muted-foreground">{formatDateTime(d.createdAt)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

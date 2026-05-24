"use client"

import { useCallback, useEffect, useState } from "react"
import {
  CheckCircle2,
  Clock,
  Loader2,
  Play,
  XCircle,
} from "lucide-react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import {
  Alert,
  AlertDescription,
} from "@/components/ui/alert"
import { api } from "@/lib/api"
import { usePortalStore } from "@/stores/usePortalStore"
import type { PortalWebhookDeliveryItem, PortalTestWebhookResponse } from "@/types/portal-api"

// ── Page ────────────────────────────────────────────────────────────────

export default function WebhooksPage() {
  const { activeMerchantId } = usePortalStore()
  const [items, setItems] = useState<PortalWebhookDeliveryItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(0)
  const [testResult, setTestResult] = useState<PortalTestWebhookResponse | null>(null)
  const [testing, setTesting] = useState(false)
  const limit = 20

  const fetchData = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    try {
      const res = await api.portal.webhookDeliveries(activeMerchantId, {
        limit,
        offset: page * limit,
      })
      setItems(res.items)
      setTotal(res.total)
    } catch {
      setItems([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId, page])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const handleTestWebhook = async () => {
    if (!activeMerchantId) return
    setTesting(true)
    setTestResult(null)
    try {
      const result = await api.portal.testWebhook(activeMerchantId)
      setTestResult(result)
      // Refresh deliveries to show the test delivery
      await fetchData()
    } catch (err) {
      setTestResult({
        success: false,
        statusCode: null,
        responseTimeMs: null,
        error: err instanceof Error ? err.message : "Unknown error",
      })
    } finally {
      setTesting(false)
    }
  }

  const totalPages = Math.ceil(total / limit)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Вебхуки</h1>
          <p className="text-sm text-muted-foreground mt-1">
            История доставки вебхук-уведомлений. Настройте URL в разделе «Настройки».
          </p>
        </div>
        <Button onClick={handleTestWebhook} disabled={testing || !activeMerchantId}>
          {testing ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <Play className="mr-2 h-4 w-4" />
          )}
          Тест вебхука
        </Button>
      </div>

      {/* Test Result Alert */}
      {testResult && (
        <Alert variant={testResult.success ? "default" : "destructive"}>
          <AlertDescription className="flex items-center gap-3 text-sm">
            {testResult.success ? (
              <CheckCircle2 className="h-4 w-4 text-green-500" />
            ) : (
              <XCircle className="h-4 w-4" />
            )}
            <div>
              {testResult.success ? "Тестовый вебхук доставлен успешно" : "Ошибка доставки"}
              {testResult.statusCode && (
                <span className="ml-2 text-muted-foreground">HTTP {testResult.statusCode}</span>
              )}
              {testResult.responseTimeMs && (
                <span className="ml-2 text-muted-foreground">{testResult.responseTimeMs}ms</span>
              )}
              {testResult.error && (
                <span className="ml-2 text-muted-foreground">{testResult.error}</span>
              )}
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="ml-auto h-6 px-2"
              onClick={() => setTestResult(null)}
            >
              ×
            </Button>
          </AlertDescription>
        </Alert>
      )}

      {!activeMerchantId && (
        <Alert>
          <AlertDescription>Выберите мерчанта для просмотра истории вебхуков.</AlertDescription>
        </Alert>
      )}

      {/* Deliveries Table */}
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Событие</TableHead>
                <TableHead>Delivery ID</TableHead>
                <TableHead>Статус</TableHead>
                <TableHead>Время ответа</TableHead>
                <TableHead>Попытка</TableHead>
                <TableHead>Дата</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 6 }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className="h-4 w-full" />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : items.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-32 text-center text-muted-foreground">
                    Нет записей о доставке вебхуков
                  </TableCell>
                </TableRow>
              ) : (
                items.map((d) => {
                  const isSuccess = d.statusCode !== null && d.statusCode >= 200 && d.statusCode < 300
                  const isFailed = d.statusCode === null || d.statusCode >= 300

                  return (
                    <TableRow key={d.id}>
                      <TableCell>
                        <Badge variant="outline" className="font-mono text-xs">
                          {d.eventType}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger className="font-mono text-xs truncate max-w-[120px] block">
                              {d.deliveryId.slice(0, 8)}…
                            </TooltipTrigger>
                            <TooltipContent>
                              <p className="font-mono text-xs">{d.deliveryId}</p>
                            </TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1.5">
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
                        </div>
                      </TableCell>
                      <TableCell className="text-sm tabular-nums">
                        {d.responseTimeMs !== null ? `${d.responseTimeMs}ms` : "—"}
                      </TableCell>
                      <TableCell className="text-sm">
                        #{d.attempt}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {new Date(d.createdAt).toLocaleDateString("ru-RU", {
                          day: "2-digit",
                          month: "2-digit",
                          year: "numeric",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <p className="text-sm text-muted-foreground">
            Страница {page + 1} из {totalPages}
          </p>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" disabled={page === 0} onClick={() => setPage(0)}>
              «
            </Button>
            <Button variant="outline" size="sm" disabled={page === 0} onClick={() => setPage(p => p - 1)}>
              ‹
            </Button>
            <Button variant="outline" size="sm" disabled={page >= totalPages - 1} onClick={() => setPage(p => p + 1)}>
              ›
            </Button>
            <Button variant="outline" size="sm" disabled={page >= totalPages - 1} onClick={() => setPage(totalPages - 1)}>
              »
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

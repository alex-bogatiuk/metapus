"use client"

import { useCallback, useEffect, useState } from "react"
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  CheckCircle2,
  Clock,
  Copy,
  ExternalLink,
  Loader2,
  Plus,
  Radio,
  Send,
  Shield,
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"
import { usePortalStore } from "@/stores/usePortalStore"
import { truncateAddress, getExplorerUrl } from "@/lib/blockchain"
import { formatMinorUnits, formatDateTime, copyToClipboard } from "@/lib/format"
import type { PortalWithdrawalItem } from "@/types/portal-api"

// ── Status Config ──────────────────────────────────────────────────────

const WITHDRAWAL_STATUS: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline"; icon: typeof CheckCircle2 }> = {
  created:   { label: "Создан",      variant: "outline",      icon: Clock },
  signed:    { label: "Подписан",    variant: "secondary",    icon: Send },
  broadcast: { label: "Отправлен",   variant: "secondary",    icon: Radio },
  confirmed: { label: "Подтверждён", variant: "default",      icon: CheckCircle2 },
  failed:    { label: "Ошибка",      variant: "destructive",  icon: XCircle },
}



// ── Sortable Head ──────────────────────────────────────────────────────

function SortableHead({
  children,
  column,
  currentSort,
  currentOrder,
  onSort,
}: {
  children: React.ReactNode
  column: string
  currentSort: string
  currentOrder: string
  onSort: (col: string) => void
}) {
  const isActive = currentSort === column
  const Icon = isActive ? (currentOrder === "asc" ? ArrowUp : ArrowDown) : ArrowUpDown
  return (
    <TableHead>
      <Button
        variant="ghost"
        size="sm"
        className="-ml-3 h-8 data-[state=active]:text-foreground"
        data-state={isActive ? "active" : "inactive"}
        onClick={() => onSort(column)}
      >
        {children}
        <Icon className="ml-1 h-3.5 w-3.5" />
      </Button>
    </TableHead>
  )
}

// ── Page ────────────────────────────────────────────────────────────────

export default function WithdrawalsPage() {
  const { activeMerchantId } = usePortalStore()
  const [items, setItems] = useState<PortalWithdrawalItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(0)
  const [statusFilter, setStatusFilter] = useState<string>("all")
  const [sort, setSort] = useState("created_at")
  const [order, setOrder] = useState<"asc" | "desc">("desc")
  const limit = 20

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.portal.withdrawals({
        merchantId: activeMerchantId || undefined,
        status: statusFilter === "all" ? undefined : statusFilter,
        sort,
        order,
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
  }, [activeMerchantId, statusFilter, sort, order, page])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const handleSort = (col: string) => {
    if (sort === col) {
      setOrder(order === "asc" ? "desc" : "asc")
    } else {
      setSort(col)
      setOrder("desc")
    }
    setPage(0)
  }

  const totalPages = Math.ceil(total / limit)


  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Выводы</h1>
          <p className="text-sm text-muted-foreground mt-1">
            История выводов средств. Выводы проходят одобрение в ERP.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" asChild>
            <a href="/portal/withdrawals/addresses">
              <Shield className="mr-2 size-4" />
              Адреса
            </a>
          </Button>
          <Button size="sm" asChild>
            <a href="/portal/withdrawals/new">
              <Plus className="mr-2 size-4" />
              Запросить вывод
            </a>
          </Button>
        </div>
      </div>

      {/* Filters */}
      <div className="flex items-center gap-3">
        <Select value={statusFilter} onValueChange={(v) => { setStatusFilter(v); setPage(0) }}>
          <SelectTrigger className="w-[180px]">
            <SelectValue placeholder="Статус" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Все статусы</SelectItem>
            <SelectItem value="created">Создан</SelectItem>
            <SelectItem value="signed">Подписан</SelectItem>
            <SelectItem value="broadcast">Отправлен</SelectItem>
            <SelectItem value="confirmed">Подтверждён</SelectItem>
            <SelectItem value="failed">Ошибка</SelectItem>
          </SelectContent>
        </Select>
        <span className="text-sm text-muted-foreground ml-auto">
          {total} {total === 1 ? "вывод" : "выводов"}
        </span>
      </div>

      {/* Table */}
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <SortableHead column="number" currentSort={sort} currentOrder={order} onSort={handleSort}>
                  Номер
                </SortableHead>
                <SortableHead column="status" currentSort={sort} currentOrder={order} onSort={handleSort}>
                  Статус
                </SortableHead>
                <SortableHead column="amount" currentSort={sort} currentOrder={order} onSort={handleSort}>
                  Сумма
                </SortableHead>
                <TableHead>Адрес</TableHead>
                <TableHead>TX Hash</TableHead>
                <SortableHead column="created_at" currentSort={sort} currentOrder={order} onSort={handleSort}>
                  Дата
                </SortableHead>
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
                    Нет выводов
                  </TableCell>
                </TableRow>
              ) : (
                items.map((w) => {
                  const statusCfg = WITHDRAWAL_STATUS[w.status] ?? WITHDRAWAL_STATUS.created
                  const StatusIcon = statusCfg.icon
                  const explorerUrl = w.txHash ? getExplorerUrl(w.txHash, w.network) : null

                  return (
                    <TableRow key={w.id}>
                      <TableCell className="font-mono text-sm">{w.number}</TableCell>
                      <TableCell>
                        <Badge variant={statusCfg.variant} className="gap-1">
                          <StatusIcon className="h-3 w-3" />
                          {statusCfg.label}
                        </Badge>
                      </TableCell>
                      <TableCell className="font-mono tabular-nums">
                        {formatMinorUnits(w.amount, w.decimalPlaces)} {w.symbol}
                      </TableCell>
                      <TableCell>
                        <TooltipProvider>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="h-auto py-0 px-1 font-mono text-xs"
                                onClick={() => copyToClipboard(w.destAddress)}
                              >
                                {truncateAddress(w.destAddress)}
                                <Copy className="ml-1 h-3 w-3 opacity-50" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>
                              <p className="font-mono text-xs">{w.destAddress}</p>
                            </TooltipContent>
                          </Tooltip>
                        </TooltipProvider>
                      </TableCell>
                      <TableCell>
                        {w.txHash ? (
                          <div className="flex items-center gap-1">
                            <span className="font-mono text-xs">{truncateAddress(w.txHash)}</span>
                            {explorerUrl && (
                              <a href={explorerUrl} target="_blank" rel="noopener noreferrer" className="text-muted-foreground hover:text-foreground">
                                <ExternalLink className="h-3 w-3" />
                              </a>
                            )}
                          </div>
                        ) : (
                          <span className="text-muted-foreground text-xs">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDateTime(w.createdAt)}
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

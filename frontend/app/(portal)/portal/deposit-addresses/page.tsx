"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import Link from "next/link"
import Image from "next/image"
import {
  AlertCircle,
  Check,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Clock,
  Copy,
  ExternalLink,
  Info,
  Loader2,
  Plus,
  QrCode,
  Search,
  Sparkles,
} from "lucide-react"
import { toast } from "sonner"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { api, ApiError } from "@/lib/api"
import { getNetworkColor, truncateAddress } from "@/configuration/crypto/lib/blockchain"
import { copyToClipboard, formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { usePortalStore } from "@/configuration/crypto/stores/usePortalStore"
import { DepositAddressRisk, DepositAddressRotationAction } from "@/configuration/crypto/components/portal/deposit-address-actions"
import type {
  PortalCreateDepositAddressResponse,
  PortalDepositAddressItem,
  PortalDepositAddressQuotaItem,
} from "@/configuration/crypto/types/portal-api"

const PAGE_SIZES = [10, 20, 50, 100]
const DEFAULT_PAGE_SIZE = 20
const PORTAL_ROLE_MANAGER = 2

export default function PortalDepositAddressesPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const merchants = usePortalStore((s) => s.merchants)

  const activeMerchant = useMemo(
    () => merchants.find((m) => m.id === activeMerchantId) ?? null,
    [merchants, activeMerchantId]
  )
  const canCreate = activeMerchant ? activeMerchant.role > 0 && activeMerchant.role <= PORTAL_ROLE_MANAGER : false

  // Data state
  const [items, setItems] = useState<PortalDepositAddressItem[]>([])
  const [quotas, setQuotas] = useState<PortalDepositAddressQuotaItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [refreshTick, setRefreshTick] = useState(0)

  // Filters & pagination
  const [search, setSearch] = useState("")
  const [debouncedSearch, setDebouncedSearch] = useState("")
  const [networkFilter, setNetworkFilter] = useState<string>("all")
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [currentPage, setCurrentPage] = useState(1)

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false)
  const [selectedNetworkId, setSelectedNetworkId] = useState<string>("")
  const [customerIdInput, setCustomerIdInput] = useState<string>("")
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<{ code?: string; message: string } | null>(null)
  const [retryCountdown, setRetryCountdown] = useState<number>(0)
  const [createdResult, setCreatedResult] = useState<PortalCreateDepositAddressResponse | null>(null)

  // Detail dialog state
  const [detailItem, setDetailItem] = useState<PortalDepositAddressItem | null>(null)

  useEffect(() => {
    if (!activeMerchantId) return
    const timer = setInterval(() => {
      if (!document.hidden) setRefreshTick(value => value + 1)
    }, 30_000)
    return () => clearInterval(timer)
  }, [activeMerchantId])

  // Reset pagination when filters change
  const filterKey = `${activeMerchantId || ""}_${debouncedSearch || ""}_${networkFilter || ""}_${pageSize}`
  const [prevFilterKey, setPrevFilterKey] = useState(filterKey)
  if (filterKey !== prevFilterKey) {
    setPrevFilterKey(filterKey)
    setCurrentPage(1)
    setLoading(true)
  }

  // Debounce search input
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(search.trim()), 300)
    return () => clearTimeout(timer)
  }, [search])

  // Countdown timer for WALLET_POOL_EXHAUSTED retry
  useEffect(() => {
    if (retryCountdown <= 0) return
    const timer = setInterval(() => {
      setRetryCountdown((prev) => (prev <= 1 ? 0 : prev - 1))
    }, 1000)
    return () => clearInterval(timer)
  }, [retryCountdown])

  // Fetch deposit addresses & quotas
  useEffect(() => {
    if (!activeMerchantId) {
      setItems([])
      setQuotas([])
      setTotal(0)
      setLoading(false)
      return
    }

    let active = true
    if (refreshTick === 0) setLoading(true)
    const offset = (currentPage - 1) * pageSize

    api.portal.depositAddresses
      .list({
        merchantId: activeMerchantId,
        networkId: networkFilter !== "all" ? networkFilter : undefined,
        search: debouncedSearch || undefined,
        limit: pageSize,
        offset,
      })
      .then((res) => {
        if (!active) return
        setItems(res.items)
        setTotal(res.total)
        setQuotas(res.quotas)
        if (!selectedNetworkId && res.quotas.length > 0) {
          setSelectedNetworkId(res.quotas[0].networkId)
        }
      })
      .catch(() => {
        if (!active) return
        setItems([])
        setTotal(0)
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
    }
  }, [activeMerchantId, debouncedSearch, networkFilter, pageSize, currentPage, refreshTick, selectedNetworkId])

  const selectedQuota = useMemo(
    () => quotas.find((q) => q.networkId === selectedNetworkId) ?? null,
    [quotas, selectedNetworkId]
  )

  const handleOpenCreate = useCallback(
    (preselectNetworkId?: string) => {
      setCreateError(null)
      setRetryCountdown(0)
      setCreatedResult(null)
      setCustomerIdInput("")
      if (preselectNetworkId) {
        setSelectedNetworkId(preselectNetworkId)
      } else if (networkFilter !== "all") {
        setSelectedNetworkId(networkFilter)
      } else if (quotas.length > 0 && !selectedNetworkId) {
        setSelectedNetworkId(quotas[0].networkId)
      }
      setCreateOpen(true)
    },
    [networkFilter, quotas, selectedNetworkId]
  )

  const handleCreate = useCallback(async () => {
    const trimmedCustomer = customerIdInput.trim()
    if (!activeMerchantId || !selectedNetworkId || !trimmedCustomer) return

    setCreating(true)
    setCreateError(null)
    try {
      const res = await api.portal.depositAddresses.create(activeMerchantId, {
        networkId: selectedNetworkId,
        customerId: trimmedCustomer,
      })
      setCreatedResult(res)
      setRefreshTick((t) => t + 1)
      if (res.reused) {
        toast.info("Возвращён ранее привязанный депозитный адрес клиента")
      } else {
        toast.success("Депозитный адрес успешно закреплён за клиентом")
      }
    } catch (err) {
      if (err instanceof ApiError) {
        const code = err.parsedBody?.code
        const details = err.parsedBody?.details
        const retrySeconds =
          typeof details?.walletPoolRetryDelaySeconds === "number"
            ? details.walletPoolRetryDelaySeconds
            : undefined

        if (code === "WALLET_POOL_EXHAUSTED") {
          const delay = retrySeconds && retrySeconds > 0 ? retrySeconds : 10
          setRetryCountdown(delay)
          setCreateError({
            code,
            message: "Пул свободных адресов пополняется. Повторите попытку через несколько секунд.",
          })
        } else if (code === "MERCHANT_PERSISTENT_ADDRESS_LIMIT_EXCEEDED") {
          setCreateError({
            code,
            message: "Исчерпан лимит постоянных депозитных адресов для выбранной сети.",
          })
        } else {
          setCreateError({
            code,
            message: err.message || "Не удалось выдать депозитный адрес",
          })
        }
      } else {
        setCreateError({
          message: err instanceof Error ? err.message : "Не удалось выдать депозитный адрес",
        })
      }
    } finally {
      setCreating(false)
    }
  }, [activeMerchantId, selectedNetworkId, customerIdInput])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const paginationRange = useMemo(() => {
    const range: number[] = []
    const delta = 2
    const left = Math.max(1, currentPage - delta)
    const right = Math.min(totalPages, currentPage + delta)

    if (left > 1) range.push(1)
    if (left > 2) range.push(-1)
    for (let i = left; i <= right; i++) range.push(i)
    if (right < totalPages - 1) range.push(-2)
    if (right < totalPages) range.push(totalPages)

    return range
  }, [currentPage, totalPages])

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <QrCode className="size-10 mb-3 opacity-40" />
        <p>Выберите мерчанта для управления депозитными адресами</p>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Page Header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Депозитные адреса</h1>
          <p className="text-sm text-muted-foreground">
            Постоянная привязка крипто-кошельков к клиентам (<code className="text-xs">customerId</code>) для автоматического приёма пополнений (Top-Up)
          </p>
        </div>

        {canCreate && (
          <Button size="sm" onClick={() => handleOpenCreate()}>
            <Plus className="mr-2 size-4" />
            Выдать адрес
          </Button>
        )}
      </div>

      {/* Network Quota Cards */}
      {quotas.length > 0 && (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {quotas.map((q) => {
            const isSelected = networkFilter === q.networkId
            const hasLimit = q.max > 0
            const pct = hasLimit ? Math.min(100, Math.round((q.used / q.max) * 100)) : 0
            const isExhausted = hasLimit && q.used >= q.max

            return (
              <Card
                key={q.networkId}
                className={cn(
                  "cursor-pointer transition-colors hover:border-primary/50",
                  isSelected && "border-primary bg-primary/5"
                )}
                onClick={() =>
                  setNetworkFilter((prev) => (prev === q.networkId ? "all" : q.networkId))
                }
              >
                <CardContent className="p-4 space-y-2.5">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <span
                        className={cn(
                          "size-2.5 rounded-full shrink-0",
                          getNetworkColor(q.networkName || q.networkCode)
                        )}
                      />
                      <span className="font-medium text-sm truncate">
                        {q.networkName || q.networkCode}
                      </span>
                      <span className="text-xs text-muted-foreground uppercase">
                        {q.networkCode}
                      </span>
                    </div>

                    <div className="flex items-center gap-1 flex-wrap justify-end">
                      {q.tokens.map((sym) => (
                        <Badge key={sym} variant="secondary" className="text-[10px] px-1.5 py-0">
                          {sym}
                        </Badge>
                      ))}
                    </div>
                  </div>

                  <div className="flex items-center justify-between text-xs text-muted-foreground">
                    <span>Выдано постоянных адресов</span>
                    <span
                      className={cn(
                        "font-mono font-medium tabular-nums",
                        isExhausted ? "text-destructive" : "text-foreground"
                      )}
                    >
                      {q.used} / {hasLimit ? q.max : "∞"}
                    </span>
                  </div>

                  {hasLimit && (
                    <div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
                      <div
                        className={cn(
                          "h-full transition-all",
                          isExhausted
                            ? "bg-destructive"
                            : pct >= 85
                              ? "bg-amber-500"
                              : "bg-primary"
                        )}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      {/* Filter Toolbar */}
      <Card>
        <CardContent className="py-3">
          <div className="flex flex-wrap items-center gap-3">
            {/* Search by customerId or address */}
            <div className="relative flex-1 min-w-[220px] max-w-[360px]">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Поиск по customerId или адресу кошелька..."
                className="pl-8 h-9"
              />
            </div>

            {/* Network Filter */}
            <Select value={networkFilter} onValueChange={setNetworkFilter}>
              <SelectTrigger className="w-[200px] h-9">
                <SelectValue placeholder="Все сети" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Все сети</SelectItem>
                {quotas.map((q) => (
                  <SelectItem key={q.networkId} value={q.networkId}>
                    <span className="flex items-center gap-2">
                      <span
                        className={cn(
                          "size-2 rounded-full shrink-0",
                          getNetworkColor(q.networkName || q.networkCode)
                        )}
                      />
                      <span>{q.networkName || q.networkCode}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {/* Count summary */}
            <div className="ml-auto text-sm text-muted-foreground tabular-nums">
              {total > 0 && `${total} адрес(ов)`}
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Main Table */}
      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center h-40">
              <Loader2 className="size-5 animate-spin text-muted-foreground" />
            </div>
          ) : items.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 px-4 text-center">
              <QrCode className="size-10 text-muted-foreground/40 mb-3" />
              <p className="text-sm font-medium">Постоянные депозитные адреса не найдены</p>
              <p className="text-xs text-muted-foreground max-w-md mt-1">
                Выдайте клиенту (<code className="text-[11px]">customerId</code>) персональный адрес в нужной сети.
                Любое пополнение поддерживаемыми токенами на этот адрес автоматически создаст инвойс пополнения (Top-Up) и зачислит средства на баланс.
              </p>
              {canCreate && (
                <Button size="sm" className="mt-4" onClick={() => handleOpenCreate()}>
                  <Plus className="mr-2 size-4" />
                  Выдать первый адрес
                </Button>
              )}
            </div>
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>ID клиента (customerId)</TableHead>
                    <TableHead>Сеть и токены</TableHead>
                    <TableHead>Депозитный адрес</TableHead>
                    <TableHead>AML-риск</TableHead>
                    <TableHead className="text-right">Пополнений</TableHead>
                    <TableHead className="hidden 2xl:table-cell">Последний депозит</TableHead>
                    <TableHead>Выдан</TableHead>
                    <TableHead className="w-[200px] text-right">Действия</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((item) => (
                    <TableRow
                      key={item.id}
                      className="group cursor-pointer hover:bg-muted/50 transition-colors"
                      onClick={() => setDetailItem(item)}
                    >
                      <TableCell className="font-mono text-sm font-medium">
                        <div className="flex items-center gap-1.5">
                          <span className="truncate max-w-[220px]" title={item.customerId}>
                            {item.customerId}
                          </span>
                          <CopyInlineButton text={item.customerId} />
                        </div>
                      </TableCell>

                      <TableCell>
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="flex items-center gap-1.5 text-sm font-medium">
                            <span
                              className={cn(
                                "size-2 rounded-full shrink-0",
                                getNetworkColor(item.networkName || item.networkCode)
                              )}
                            />
                            {item.networkName || item.networkCode}
                          </span>
                          <div className="flex items-center gap-1">
                            {item.tokens.map((sym) => (
                              <Badge
                                key={sym}
                                variant="outline"
                                className="text-[10px] px-1.5 py-0"
                              >
                                {sym}
                              </Badge>
                            ))}
                          </div>
                        </div>
                      </TableCell>

                      <TableCell className="font-mono text-sm">
                        <div className="flex items-center gap-1.5">
                          <span title={item.address}>
                            {truncateAddress(item.address, 8, 6)}
                          </span>
                          <CopyInlineButton text={item.address} />
                        </div>
                      </TableCell>

                      <TableCell onClick={event => event.stopPropagation()}>
                        <DepositAddressRisk item={item} />
                        {item.riskClass === "high" && <p className="text-xs text-destructive">Рекомендуется замена</p>}
                      </TableCell>

                      <TableCell className="text-right tabular-nums text-sm">
                        {item.topUpCount > 0 ? (
                          <Link
                            href={`/portal/invoices?search=${encodeURIComponent(item.customerId)}`}
                            onClick={(e) => e.stopPropagation()}
                            className="inline-flex items-center gap-1 font-medium text-primary hover:underline"
                          >
                            {item.topUpCount}
                            <ExternalLink className="size-3" />
                          </Link>
                        ) : (
                          <span className="text-muted-foreground">0</span>
                        )}
                      </TableCell>

                      <TableCell className="hidden text-sm text-muted-foreground 2xl:table-cell">
                        {item.lastTopUpAt ? formatDateTime(item.lastTopUpAt) : "—"}
                      </TableCell>

                      <TableCell className="text-sm text-muted-foreground">
                        {formatDateTime(item.createdAt)}
                      </TableCell>

                      <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="flex items-center justify-end gap-1">
                          {canCreate && activeMerchantId && <DepositAddressRotationAction item={item} merchantId={activeMerchantId} onRotated={result => {
                            setDetailItem(null)
                            setCreatedResult(result)
                            setCreateOpen(true)
                            setRefreshTick(value => value + 1)
                          }} />}
                          <TooltipProvider delayDuration={0}>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  className="size-8"
                                  onClick={() => setDetailItem(item)}
                                >
                                  <QrCode className="size-4" />
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-xs">
                                Реквизиты адреса
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>

                          <TooltipProvider delayDuration={0}>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button variant="ghost" size="icon" className="size-8" asChild>
                                  <Link
                                    href={`/portal/invoices?search=${encodeURIComponent(item.customerId)}`}
                                  >
                                    <ExternalLink className="size-4" />
                                  </Link>
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-xs">
                                Инвойсы клиента
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {/* Pagination */}
              <div className="flex items-center justify-between px-4 py-3 border-t">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <span>Строк:</span>
                  <Select
                    value={String(pageSize)}
                    onValueChange={(v) => setPageSize(Number(v))}
                  >
                    <SelectTrigger className="w-[70px] h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {PAGE_SIZES.map((s) => (
                        <SelectItem key={s} value={String(s)}>
                          {s}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-8"
                    disabled={currentPage <= 1}
                    onClick={() => setCurrentPage(1)}
                  >
                    <ChevronsLeft className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-8"
                    disabled={currentPage <= 1}
                    onClick={() => setCurrentPage((p) => p - 1)}
                  >
                    <ChevronLeft className="size-4" />
                  </Button>

                  {paginationRange.map((page, idx) =>
                    page < 0 ? (
                      <span
                        key={`ellipsis-${idx}`}
                        className="px-1 text-muted-foreground"
                      >
                        …
                      </span>
                    ) : (
                      <Button
                        key={page}
                        variant={page === currentPage ? "default" : "ghost"}
                        size="icon"
                        className="size-8 text-xs"
                        onClick={() => setCurrentPage(page)}
                      >
                        {page}
                      </Button>
                    )
                  )}

                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-8"
                    disabled={currentPage >= totalPages}
                    onClick={() => setCurrentPage((p) => p + 1)}
                  >
                    <ChevronRight className="size-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-8"
                    disabled={currentPage >= totalPages}
                    onClick={() => setCurrentPage(totalPages)}
                  >
                    <ChevronsRight className="size-4" />
                  </Button>
                </div>

                <p className="text-xs text-muted-foreground tabular-nums">
                  {(currentPage - 1) * pageSize + 1}–{Math.min(currentPage * pageSize, total)} из {total}
                </p>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Create / Bind Deposit Address Dialog */}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-lg">
          {!createdResult ? (
            <>
              <DialogHeader>
                <DialogTitle>Выдать постоянный депозитный адрес</DialogTitle>
                <DialogDescription>
                  Закрепите адрес в выбранной блокчейн-сети за клиентом (<code className="text-xs">customerId</code>).
                </DialogDescription>
              </DialogHeader>

              <div className="space-y-4 py-2">
                {/* Network Selection */}
                <div className="space-y-2">
                  <Label>Блокчейн-сеть *</Label>
                  <Select value={selectedNetworkId} onValueChange={setSelectedNetworkId}>
                    <SelectTrigger>
                      <SelectValue placeholder="Выберите блокчейн-сеть" />
                    </SelectTrigger>
                    <SelectContent>
                      {quotas.map((q) => (
                        <SelectItem key={q.networkId} value={q.networkId}>
                          <div className="flex items-center gap-2">
                            <span
                              className={cn(
                                "size-2 rounded-full shrink-0",
                                getNetworkColor(q.networkName || q.networkCode)
                              )}
                            />
                            <span>{q.networkName || q.networkCode}</span>
                            <span className="text-xs text-muted-foreground uppercase">
                              ({q.tokens.join(", ")})
                            </span>
                          </div>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>

                  {selectedQuota && (
                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                      <span>
                        Токены сети:{" "}
                        <strong className="text-foreground">
                          {selectedQuota.tokens.length > 0
                            ? selectedQuota.tokens.join(", ")
                            : "—"}
                        </strong>
                      </span>
                      <span className="font-mono">
                        Квота: {selectedQuota.used} /{" "}
                        {selectedQuota.max > 0 ? selectedQuota.max : "∞"}
                      </span>
                    </div>
                  )}
                </div>

                {/* Customer ID Input */}
                <div className="space-y-2">
                  <Label htmlFor="customer-id-input">ID клиента (customerId) *</Label>
                  <Input
                    id="customer-id-input"
                    placeholder="Например: user_10492 или email@domain.com"
                    value={customerIdInput}
                    maxLength={255}
                    onChange={(e) => setCustomerIdInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (
                        e.key === "Enter" &&
                        customerIdInput.trim() &&
                        selectedNetworkId &&
                        !creating &&
                        retryCountdown === 0
                      ) {
                        e.preventDefault()
                        void handleCreate()
                      }
                    }}
                  />
                  <p className="text-[11px] text-muted-foreground">
                    Уникальный идентификатор клиента в вашей системе (до 255 символов).
                  </p>
                </div>

                {/* Idempotency & Multi-token Info Callout */}
                <div className="rounded-md border bg-muted/40 p-3 text-xs text-muted-foreground space-y-1.5">
                  <div className="flex items-start gap-2">
                    <Info className="size-4 text-primary shrink-0 mt-0.5" />
                    <div className="space-y-1">
                      <p className="font-medium text-foreground">
                        Идемпотентная привязка на уровне сети
                      </p>
                      <p>
                        Если у клиента с этим <code className="text-[11px]">customerId</code> уже есть постоянный адрес в выбранной сети, система вернёт существующий адрес без расхода квоты.
                      </p>
                    </div>
                  </div>
                </div>

                {/* Error Alert */}
                {createError && (
                  <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-xs text-destructive flex items-start gap-2">
                    <AlertCircle className="size-4 shrink-0 mt-0.5" />
                    <div className="space-y-1">
                      <p className="font-medium">{createError.message}</p>
                      {retryCountdown > 0 && (
                        <p className="flex items-center gap-1.5 text-muted-foreground">
                          <Clock className="size-3.5" />
                          Повторная попытка будет доступна через {retryCountdown} сек.
                        </p>
                      )}
                    </div>
                  </div>
                )}
              </div>

              <DialogFooter>
                <Button variant="outline" onClick={() => setCreateOpen(false)}>
                  Отмена
                </Button>
                <Button
                  onClick={handleCreate}
                  disabled={
                    creating ||
                    !selectedNetworkId ||
                    !customerIdInput.trim() ||
                    retryCountdown > 0
                  }
                >
                  {creating && <Loader2 className="mr-2 size-4 animate-spin" />}
                  {retryCountdown > 0
                    ? `Повторить (${retryCountdown}с)`
                    : "Получить адрес"}
                </Button>
              </DialogFooter>
            </>
          ) : (
            <>
              <DialogHeader>
                <div className="flex items-center gap-2">
                  <DialogTitle>Депозитный адрес клиента</DialogTitle>
                  <Badge variant={createdResult.reused ? "secondary" : "success"} className="gap-1">
                    {createdResult.reused ? (
                      <>
                        <CheckCircle2 className="size-3" />
                        Уже привязан
                      </>
                    ) : (
                      <>
                        <Sparkles className="size-3" />
                        Новый адрес
                      </>
                    )}
                  </Badge>
                </div>
                <DialogDescription>
                  {createdResult.reused
                    ? "Для этого customerId в выбранной сети уже существовала постоянная привязка."
                    : "Кошелёк закреплён за клиентом на постоянной основе."}
                </DialogDescription>
              </DialogHeader>

              <DepositAddressDetailsView
                customerId={createdResult.customerId}
                address={createdResult.address}
                networkName={createdResult.networkName || createdResult.networkCode}
                networkCode={createdResult.networkCode}
                tokens={createdResult.tokens}
                createdAt={createdResult.createdAt}
                qrCode={createdResult.qrCode}
              />

              <DialogFooter className="sm:justify-between">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setCreatedResult(null)
                    setCustomerIdInput("")
                  }}
                >
                  Выдать ещё один адрес
                </Button>
                <Button onClick={() => setCreateOpen(false)}>Готово</Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>

      {/* Existing Deposit Address Details Dialog */}
      <Dialog open={!!detailItem} onOpenChange={(open) => !open && setDetailItem(null)}>
        <DialogContent className="sm:max-w-lg">
          {detailItem && (
            <>
              <DialogHeader>
                <DialogTitle>Реквизиты депозитного адреса</DialogTitle>
                <DialogDescription>
                  Постоянный адрес пополнения для клиента{" "}
                  <code className="font-mono text-xs text-foreground">{detailItem.customerId}</code>
                </DialogDescription>
              </DialogHeader>

              <DepositAddressDetailsView
                customerId={detailItem.customerId}
                address={detailItem.address}
                networkName={detailItem.networkName || detailItem.networkCode}
                networkCode={detailItem.networkCode}
                tokens={detailItem.tokens}
                createdAt={detailItem.createdAt}
                topUpCount={detailItem.topUpCount}
                lastTopUpAt={detailItem.lastTopUpAt}
              />

              <DialogFooter className="sm:justify-between">
                <Button variant="outline" size="sm" asChild>
                  <Link
                    href={`/portal/invoices?search=${encodeURIComponent(detailItem.customerId)}`}
                  >
                    <ExternalLink className="mr-2 size-4" />
                    История пополнений ({detailItem.topUpCount})
                  </Link>
                </Button>
                <Button onClick={() => setDetailItem(null)}>Закрыть</Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

// ── Shared Address Details View ────────────────────────────────────────────

interface DepositAddressDetailsViewProps {
  customerId: string
  address: string
  networkName: string
  networkCode: string
  tokens: string[]
  createdAt: string
  topUpCount?: number
  lastTopUpAt?: string
  qrCode?: string
}

function DepositAddressDetailsView({
  customerId,
  address,
  networkName,
  networkCode,
  tokens,
  createdAt,
  topUpCount,
  lastTopUpAt,
  qrCode,
}: DepositAddressDetailsViewProps) {
  const [copiedAddr, setCopiedAddr] = useState(false)
  const [copiedCustomer, setCopiedCustomer] = useState(false)

  const handleCopyAddress = () => {
    copyToClipboard(address)
    setCopiedAddr(true)
    toast.success("Депозитный адрес скопирован")
    setTimeout(() => setCopiedAddr(false), 1800)
  }

  const handleCopyCustomer = () => {
    copyToClipboard(customerId)
    setCopiedCustomer(true)
    toast.success("customerId скопирован")
    setTimeout(() => setCopiedCustomer(false), 1800)
  }

  return (
    <div className="space-y-4 py-2">
      {qrCode && <div className="flex justify-center"><Image src={qrCode} alt={`QR-код депозитного адреса ${customerId}`} width={192} height={192} unoptimized /></div>}
      {/* Address Box */}
      <div className="rounded-lg border bg-muted/40 p-3.5 space-y-2">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>Депозитный адрес ({networkName})</span>
          <span className="uppercase font-mono">{networkCode}</span>
        </div>
        <code className="block break-all font-mono text-sm font-semibold text-foreground select-all">
          {address}
        </code>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="w-full"
          onClick={handleCopyAddress}
        >
          {copiedAddr ? (
            <>
              <Check className="mr-2 size-4 text-emerald-500" />
              Адрес скопирован
            </>
          ) : (
            <>
              <Copy className="mr-2 size-4" />
              Скопировать адрес
            </>
          )}
        </Button>
      </div>

      {/* Metadata Grid */}
      <div className="grid gap-3 text-sm">
        <div className="flex items-center justify-between border-b pb-2">
          <span className="text-muted-foreground">ID клиента (customerId)</span>
          <div className="flex items-center gap-1.5 font-mono font-medium">
            <span>{customerId}</span>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="size-6"
              onClick={handleCopyCustomer}
            >
              {copiedCustomer ? (
                <Check className="size-3.5 text-emerald-500" />
              ) : (
                <Copy className="size-3.5" />
              )}
            </Button>
          </div>
        </div>

        <div className="flex items-center justify-between border-b pb-2">
          <span className="text-muted-foreground">Сеть</span>
          <span className="flex items-center gap-1.5 font-medium">
            <span className={cn("size-2 rounded-full", getNetworkColor(networkName))} />
            {networkName}
          </span>
        </div>

        <div className="flex items-center justify-between border-b pb-2">
          <span className="text-muted-foreground">Поддерживаемые токены</span>
          <div className="flex items-center gap-1 flex-wrap justify-end">
            {tokens.map((sym) => (
              <Badge key={sym} variant="secondary" className="text-xs">
                {sym}
              </Badge>
            ))}
          </div>
        </div>

        {topUpCount !== undefined && (
          <div className="flex items-center justify-between border-b pb-2">
            <span className="text-muted-foreground">Всего пополнений (Top-Up)</span>
            <span className="font-mono font-medium tabular-nums">{topUpCount}</span>
          </div>
        )}

        {lastTopUpAt && (
          <div className="flex items-center justify-between border-b pb-2">
            <span className="text-muted-foreground">Последнее пополнение</span>
            <span className="text-muted-foreground">{formatDateTime(lastTopUpAt)}</span>
          </div>
        )}

        <div className="flex items-center justify-between">
          <span className="text-muted-foreground">Дата привязки</span>
          <span className="text-muted-foreground">{formatDateTime(createdAt)}</span>
        </div>
      </div>

      {/* Warning Note */}
      <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-800 dark:text-amber-300">
        Принимаются только токены <strong>{tokens.join(", ") || networkCode}</strong> в сети{" "}
        <strong>{networkName}</strong>. Любое входящее зачисление автоматически создаёт инвойс{" "}
        пополнения (Top-Up) и отправляет стандартные вебхуки.
      </div>
    </div>
  )
}

// ── Inline Copy Button ─────────────────────────────────────────────────────

function CopyInlineButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation()
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    },
    [text]
  )

  return (
    <TooltipProvider delayDuration={0}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={handleCopy}
            className="text-muted-foreground hover:text-foreground transition-colors opacity-0 group-hover:opacity-100 focus:opacity-100"
          >
            <Copy className={cn("size-3.5", copied && "text-emerald-500")} />
          </button>
        </TooltipTrigger>
        <TooltipContent side="top" className="text-xs">
          {copied ? "Скопировано!" : "Копировать"}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import Link from "next/link"
import {
  AlertCircle,
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  CheckCircle2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Clock,
  Copy,
  Download,
  ExternalLink,
  Loader2,
  Plus,
  Search,
  XCircle,
} from "lucide-react"

import { Card, CardContent } from "@/components/ui/card"
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
import { Input } from "@/components/ui/input"
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
import { AccountingPeriodPicker, type DateRangeValue } from "@/components/ui/accounting-period-picker"
import { formatLocalYYYYMMDD } from "@/lib/filter-utils"
import { api } from "@/lib/api"
import { usePortalStore } from "@/configuration/crypto/stores/usePortalStore"
import { cn } from "@/lib/utils"
import type { PortalInvoiceItem, PortalInvoiceDetailResponse, PortalCurrencyItem } from "@/configuration/crypto/types/portal-api"
import { buildTransactionExplorerUrl, getNetworkColor } from "@/configuration/crypto/lib/blockchain"

// ── Constants ──────────────────────────────────────────────────────────────

const STATUS_CONFIG: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline" | "success"; icon: typeof CheckCircle2 }> = {
  created: { label: "Создан", variant: "outline", icon: Clock },
  partially_paid: { label: "Частично", variant: "secondary", icon: Clock },
  paid: { label: "Оплачен", variant: "default", icon: CheckCircle2 },
  confirmed: { label: "Подтверждён", variant: "default", icon: CheckCircle2 },
  completed: { label: "Завершён", variant: "success", icon: CheckCircle2 },
  underpaid: { label: "Недоплата", variant: "secondary", icon: AlertCircle },
  reversed: { label: "Отозван", variant: "destructive", icon: AlertCircle },
  expired: { label: "Истёк", variant: "destructive", icon: XCircle },
  cancelled: { label: "Отменён", variant: "destructive", icon: XCircle },
}



const PAGE_SIZES = [10, 20, 50, 100]
const DEFAULT_PAGE_SIZE = 20



type SortField = "created_at" | "number" | "status" | "amount" | "received_amount"
type SortOrder = "asc" | "desc"

// ── Main Page ──────────────────────────────────────────────────────────────

export default function PortalInvoicesPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const searchParams = useSearchParams()
  const initialSearch = searchParams.get("search") ?? ""

  // Data
  const [items, setItems] = useState<PortalInvoiceItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)

  // Filters
  const [search, setSearch] = useState(initialSearch)
  const [debouncedSearch, setDebouncedSearch] = useState(initialSearch)
  const [statusFilter, setStatusFilter] = useState<string>("all")
  const [tokenFilter, setTokenFilter] = useState<string>("all")
  const [period, setPeriod] = useState<DateRangeValue>({})

  // Token list for filter dropdown (from currencies endpoint)
  const [tokens, setTokens] = useState<PortalCurrencyItem[]>([])

  // Fiat rate map (from balance endpoint)

  // Pagination
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [currentPage, setCurrentPage] = useState(1)

  // Sorting
  const [sortField, setSortField] = useState<SortField>("created_at")
  const [sortOrder, setSortOrder] = useState<SortOrder>("desc")

  // Expandable rows
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())

  // Render-phase state adjustments to avoid useEffect setState cascading renders
  const periodKey = `${period.from?.getTime() || ""}-${period.to?.getTime() || ""}`
  const filterKey = `${activeMerchantId || ""}_${debouncedSearch || ""}_${statusFilter || ""}_${tokenFilter || ""}_${periodKey}_${pageSize}`
  const [prevFilterKey, setPrevFilterKey] = useState(filterKey)

  if (filterKey !== prevFilterKey) {
    setPrevFilterKey(filterKey)
    setCurrentPage(1)
    setLoading(true)
  }

  const tokensKey = tokens.map((t) => `${t.symbol}-${t.network}`).join(",")
  const fetchKey = `${filterKey}_${tokensKey}_${currentPage}_${sortField}_${sortOrder}`
  const [prevFetchKey, setPrevFetchKey] = useState(fetchKey)

  if (fetchKey !== prevFetchKey) {
    setPrevFetchKey(fetchKey)
    setLoading(true)
  }

  // Debounce search
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedSearch(search), 300)
    return () => clearTimeout(timer)
  }, [search])

  // Fetch token list for the active merchant.
  useEffect(() => {
    api.portal.currencies(activeMerchantId ?? undefined)
      .then((res) => setTokens(res.items))
      .catch(() => setTokens([]))


  }, [activeMerchantId])

  // Fetch data
  useEffect(() => {
    const offset = (currentPage - 1) * pageSize

    // Find the tokenId for the selected token filter
    const selectedToken = tokenFilter !== "all"
      ? tokens.find((t) => `${t.symbol}-${t.network}` === tokenFilter)
      : undefined

    api.portal.invoices({
      merchantId: activeMerchantId ?? undefined,
      status: statusFilter !== "all" ? statusFilter : undefined,
      search: debouncedSearch || undefined,
      token: selectedToken ? selectedToken.symbol : undefined,
      dateFrom: period.from ? formatLocalYYYYMMDD(period.from) : undefined,
      dateTo: period.to ? formatLocalYYYYMMDD(period.to) : undefined,
      sort: sortField,
      order: sortOrder,
      limit: pageSize,
      offset,
    })
      .then((res) => {
        // Client-side token filter (currencies endpoint doesn't expose tokenId)
        let filtered = res.items
        if (selectedToken) {
          filtered = res.items.filter(
            (i) => i.symbol === selectedToken.symbol && i.network === selectedToken.network
          )
        }
        setItems(filtered)
        setTotal(selectedToken ? filtered.length : res.total)
      })
      .catch(() => {
        setItems([])
        setTotal(0)
      })
      .finally(() => setLoading(false))
  }, [activeMerchantId, debouncedSearch, statusFilter, tokenFilter, tokens, period, pageSize, currentPage, sortField, sortOrder])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const handleSort = useCallback((field: SortField) => {
    if (sortField === field) {
      setSortOrder((o) => (o === "asc" ? "desc" : "asc"))
    } else {
      setSortField(field)
      setSortOrder("desc")
    }
  }, [sortField])

  const toggleExpand = useCallback((id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const handleExportCsv = useCallback(async () => {
    try {
      await api.portal.exportInvoicesCsv({
        merchantId: activeMerchantId ?? undefined,
        status: statusFilter !== "all" ? statusFilter : undefined,
        search: debouncedSearch || undefined,
        dateFrom: period.from ? formatLocalYYYYMMDD(period.from) : undefined,
        dateTo: period.to ? formatLocalYYYYMMDD(period.to) : undefined,
      })
    } catch {
      // Error is already handled by ApiError in portalFetchBlob
    }
  }, [activeMerchantId, statusFilter, debouncedSearch, period])

  // Pagination range
  const paginationRange = useMemo(() => {
    const range: number[] = []
    const delta = 2
    const left = Math.max(1, currentPage - delta)
    const right = Math.min(totalPages, currentPage + delta)

    if (left > 1) range.push(1)
    if (left > 2) range.push(-1) // ellipsis
    for (let i = left; i <= right; i++) range.push(i)
    if (right < totalPages - 1) range.push(-2) // ellipsis
    if (right < totalPages) range.push(totalPages)

    return range
  }, [currentPage, totalPages])

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Инвойсы</h1>
          <p className="text-sm text-muted-foreground">
            Все платёжные инвойсы мерчанта
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleExportCsv}>
            <Download className="mr-2 size-4" />
            Экспорт CSV
          </Button>
          <Button size="sm" asChild>
            <Link href="/portal/invoices/new">
              <Plus className="mr-2 size-4" />
              Создать инвойс
            </Link>
          </Button>
        </div>
      </div>

      {/* Filter Toolbar */}
      <Card>
        <CardContent className="py-3">
          <div className="flex flex-wrap items-center gap-3">
            {/* Search */}
            <div className="relative flex-1 min-w-[200px] max-w-[320px]">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Поиск по номеру, ID клиента..."
                className="pl-8 h-9"
              />
            </div>

            {/* Status */}
            <Select value={statusFilter} onValueChange={setStatusFilter}>
              <SelectTrigger className="w-[160px] h-9">
                <SelectValue placeholder="Все статусы" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Все статусы</SelectItem>
                {Object.entries(STATUS_CONFIG).map(([status, config]) => (
                  <SelectItem key={status} value={status}>{config.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>

            {/* Token */}
            {tokens.length > 0 && (
              <Select value={tokenFilter} onValueChange={setTokenFilter}>
                <SelectTrigger className="w-[180px] h-9">
                  <SelectValue placeholder="Все токены" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Все токены</SelectItem>
                  {tokens.map((t) => (
                    <SelectItem key={`${t.symbol}-${t.network}`} value={`${t.symbol}-${t.network}`}>
                      <span className="flex items-center gap-2">
                        <span className={cn("size-2 rounded-full shrink-0", getNetworkColor(t.network))} />
                        {t.symbol}
                        <span className="text-muted-foreground text-xs">{t.network}</span>
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}

            {/* Period */}
            <AccountingPeriodPicker
              value={period}
              onChange={setPeriod}
              placeholder="Период"
              className="h-9"
            />

            {/* Summary */}
            <div className="ml-auto text-sm text-muted-foreground tabular-nums">
              {total > 0 && `${total} инвойс(ов)`}
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Table */}
      <Card>
        <CardContent className="p-0">
          {loading ? (
            <div className="flex items-center justify-center h-40">
              <Loader2 className="size-5 animate-spin text-muted-foreground" />
            </div>
          ) : items.length === 0 ? (
            <p className="text-sm text-muted-foreground py-12 text-center">
              Нет инвойсов
            </p>
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-8" />
                    <SortableHead field="number" label="Номер" current={sortField} order={sortOrder} onSort={handleSort} />
                    <SortableHead field="status" label="Статус" current={sortField} order={sortOrder} onSort={handleSort} />
                    <TableHead>Токен</TableHead>
                    <SortableHead field="amount" label="Сумма" current={sortField} order={sortOrder} onSort={handleSort} className="text-right" />
                    <SortableHead field="received_amount" label="Получено" current={sortField} order={sortOrder} onSort={handleSort} className="text-right" />
                    <SortableHead field="created_at" label="Дата" current={sortField} order={sortOrder} onSort={handleSort} />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((inv) => (
                    <InvoiceRow
                      key={inv.id}
                      invoice={inv}
                      merchantId={activeMerchantId}
                      expanded={expandedIds.has(inv.id)}
                      onToggle={() => toggleExpand(inv.id)}
                    />
                  ))}
                </TableBody>
              </Table>

              {/* Pagination */}
              <div className="flex items-center justify-between px-4 py-3 border-t">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <span>Строк:</span>
                  <Select value={String(pageSize)} onValueChange={(v) => setPageSize(Number(v))}>
                    <SelectTrigger className="w-[70px] h-8">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {PAGE_SIZES.map((s) => (
                        <SelectItem key={s} value={String(s)}>{s}</SelectItem>
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
                      <span key={`ellipsis-${idx}`} className="px-1 text-muted-foreground">…</span>
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
    </div>
  )
}

// ── Sortable Header ────────────────────────────────────────────────────────

interface SortableHeadProps {
  field: SortField
  label: string
  current: SortField
  order: SortOrder
  onSort: (f: SortField) => void
  className?: string
}

function SortableHead({ field, label, current, order, onSort, className }: SortableHeadProps) {
  const isActive = current === field
  const Icon = isActive ? (order === "asc" ? ArrowUp : ArrowDown) : ArrowUpDown

  return (
    <TableHead className={className}>
      <button
        type="button"
        className="inline-flex items-center gap-1 hover:text-foreground transition-colors -ml-1 px-1"
        onClick={() => onSort(field)}
      >
        {label}
        <Icon className={cn("size-3.5", isActive ? "text-foreground" : "text-muted-foreground/50")} />
      </button>
    </TableHead>
  )
}

// ── Invoice Row with Expandable Details ────────────────────────────────────

interface InvoiceRowProps {
  invoice: PortalInvoiceItem
  merchantId: string | null
  expanded: boolean
  onToggle: () => void
}

function InvoiceRow({ invoice: inv, merchantId, expanded, onToggle }: InvoiceRowProps) {
  const router = useRouter()
  const statusCfg = STATUS_CONFIG[inv.status]
  const StatusIcon = statusCfg?.icon ?? Clock
  const hasDetails = inv.paymentCount > 0
  const networkColor = getNetworkColor(inv.network)

  return (
    <>
      <TableRow
        className={cn("group cursor-pointer hover:bg-muted/50 transition-colors", expanded && "bg-muted/30")}
        onClick={() => router.push(`/portal/invoices/${inv.id}`)}
      >
        <TableCell className="w-8 px-2">
          {hasDetails && (
            <Button variant="ghost" size="icon" className="size-6" onClick={(e) => { e.stopPropagation(); onToggle() }}>
              <ChevronDown className={cn("size-4 transition-transform", expanded && "rotate-180")} />
            </Button>
          )}
        </TableCell>

        <TableCell className="font-mono text-sm">
          <div className="flex items-center gap-1.5">
            <span>#{inv.number}</span>
            <CopyButton text={inv.number} />
            {inv.invoiceKind === "top_up" && (
              <Badge variant="outline" className="text-[10px] font-sans px-1.5 py-0">
                Top-Up
              </Badge>
            )}
          </div>
          {inv.orderId && (
            <div className="text-xs text-muted-foreground font-mono truncate max-w-[200px]" title={inv.orderId}>
              {inv.orderId}
            </div>
          )}
          {inv.externalId && (
            <div className="text-xs text-muted-foreground font-mono truncate max-w-[200px]" title={inv.externalId}>
              {inv.externalId}
            </div>
          )}
          {inv.customerId && (
            <div className="text-xs text-muted-foreground font-mono truncate max-w-[200px]" title={inv.customerId}>
              {inv.customerId}
            </div>
          )}
        </TableCell>

        <TableCell>
          <Badge variant={statusCfg?.variant ?? "secondary"} className="text-[10px] gap-1">
            <StatusIcon className="size-3" />
            {statusCfg?.label ?? inv.status}
          </Badge>
        </TableCell>

        <TableCell>
          <span className="flex items-center gap-1.5">
            <span className={cn("size-2 rounded-full shrink-0", networkColor)} />
            <span className="text-sm font-medium">{inv.symbol}</span>
            <span className="text-[10px] text-muted-foreground">{inv.network}</span>
          </span>
        </TableCell>

        <TableCell className="text-right tabular-nums text-sm">
          <div>{formatWithDecimals(inv.amount, inv.decimalPlaces)}</div>
        </TableCell>

        <TableCell className="text-right tabular-nums text-sm">
          <div className={cn(
            inv.receivedAmount !== "0" && inv.receivedAmount !== inv.amount && "text-amber-600 dark:text-amber-400",
            inv.receivedAmount === inv.amount && "text-emerald-600 dark:text-emerald-400",
          )}>
            {formatWithDecimals(inv.receivedAmount, inv.decimalPlaces)}
          </div>
        </TableCell>

        <TableCell className="text-sm text-muted-foreground">
          {formatDate(inv.createdAt)}
        </TableCell>
      </TableRow>
      {hasDetails && expanded && (
        <tr>
          <td colSpan={7} className="p-0">
            <div className="bg-muted/20 border-b px-8 py-3">
              <ExpandedDetails invoice={inv} merchantId={merchantId} />
            </div>
          </td>
        </tr>
      )}
    </>
  )
}

// ── Expanded Row Details ───────────────────────────────────────────────────

function ExpandedDetails({ invoice: inv, merchantId }: { invoice: PortalInvoiceItem; merchantId: string | null }) {
  const [detail, setDetail] = useState<PortalInvoiceDetailResponse | null>(null)
  const [error, setError] = useState(false)
  const summary = detail ?? inv

  useEffect(() => {
    let active = true
    api.portal.invoiceDetail(inv.id, merchantId ?? undefined)
      .then((result) => { if (active) setDetail(result) })
      .catch(() => { if (active) setError(true) })
    return () => { active = false }
  }, [inv.id, merchantId])

  return (
    <div className="space-y-3 text-sm">
      <div className="flex flex-wrap gap-x-8 gap-y-2">
        <span>Платежей: <strong>{summary.paymentCount}</strong></span>
        <span>Комиссия: <strong>{formatWithDecimals(summary.processingFee, summary.decimalPlaces)} {summary.symbol}</strong></span>
        <span>Расчётно к зачислению: <strong className="text-emerald-600 dark:text-emerald-400">{formatWithDecimals(summary.netAmount, summary.decimalPlaces)} {summary.symbol}</strong></span>
      </div>
      {!detail && !error && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
      {error && <p className="text-destructive">Не удалось загрузить платежи</p>}
      {detail?.payments.map((payment) => {
        const explorerUrl = buildTransactionExplorerUrl(payment.txHash, inv.explorerUrl)
        return (
          <div key={payment.id} className="grid gap-2 border-t pt-3 md:grid-cols-[minmax(0,1fr)_auto_auto] md:items-center">
            <div className="flex min-w-0 items-center gap-1.5">
              <span className="font-mono text-xs truncate" title={payment.txHash}>{payment.txHash.slice(0, 10)}…{payment.txHash.slice(-8)}</span>
              <CopyButton text={payment.txHash} />
              {explorerUrl && <a href={explorerUrl} target="_blank" rel="noopener noreferrer" aria-label="Открыть транзакцию" className="text-muted-foreground hover:text-foreground"><ExternalLink className="size-3.5" /></a>}
            </div>
            <span className="tabular-nums">{formatWithDecimals(payment.amount, inv.decimalPlaces)} {inv.symbol}</span>
            <span className="text-muted-foreground">{payment.canonicality !== "canonical" || ["reorged", "replaced", "failed"].includes(payment.status) ? "Исключён" : payment.posted ? "Зачислен" : ["confirmed", "settled"].includes(payment.status) ? "Ожидает зачисления" : "Ожидает подтверждения"}</span>
          </div>
        )
      })}
    </div>
  )
}

// ── Copy Button ────────────────────────────────────────────────────────────

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(async (e: React.MouseEvent) => {
    e.stopPropagation()
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }, [text])

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

// ── Helpers ────────────────────────────────────────────────────────────────

function formatDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString("ru-RU", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

function formatWithDecimals(minor: string, decimals: number): string {
  if (!minor) return "0"
  if (decimals === 0) return BigInt(minor).toLocaleString("ru-RU")
  const n = BigInt(minor)
  const divisor = BigInt(10 ** decimals)
  const whole = n / divisor
  const frac = (n < BigInt(0) ? -n % divisor : n % divisor).toString().padStart(decimals, "0")
  return `${whole.toLocaleString("ru-RU")}.${frac}`
}


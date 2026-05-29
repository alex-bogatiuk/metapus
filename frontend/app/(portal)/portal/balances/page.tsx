"use client"

import { useEffect, useState } from "react"
import {
  AlertCircle,
  ArrowDownRight,
  ArrowUpRight,
  Clock,
  Loader2,
  TrendingUp,
  Wallet,
} from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
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
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import type {
  PortalDetailedBalanceResponse,
  PortalTokenDetailed,
} from "@/types/portal-api"

export default function BalancesPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [prevMerchantId, setPrevMerchantId] = useState<string | null>(null)
  const [data, setData] = useState<PortalDetailedBalanceResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  if (activeMerchantId !== prevMerchantId) {
    setPrevMerchantId(activeMerchantId)
    setLoading(true)
    setError(false)
  }

  useEffect(() => {
    api.portal
      .balanceDetailed(activeMerchantId ?? undefined)
      .then((res) => {
        setData(res)
        setError(false)
      })
      .catch(() => {
        setData(null)
        setError(true)
      })
      .finally(() => setLoading(false))
  }, [activeMerchantId])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-24">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (error || !data) {
    return (
      <div className="flex flex-col items-center justify-center py-24 gap-2 text-muted-foreground">
        <AlertCircle className="size-6" />
        <p className="text-sm">Не удалось загрузить баланс</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Баланс</h1>
          <p className="text-sm text-muted-foreground">
            Детализация по токенам: доступно, в ожидании, всего
          </p>
        </div>
        {data.rateSource && (
          <Badge variant="outline" className="text-xs font-normal capitalize">
            {data.rateSource}
          </Badge>
        )}
      </div>

      {/* Three-bucket cards */}
      <div className="grid gap-4 md:grid-cols-3">
        <BucketCard
          title="Доступно"
          value={data.availableBase}
          currency={data.baseCurrency}
          icon={<Wallet className="size-4" />}
          color="text-emerald-500"
          bg="bg-emerald-500/10"
        />
        <BucketCard
          title="В ожидании"
          value={data.pendingBase}
          currency={data.baseCurrency}
          icon={<Clock className="size-4" />}
          color="text-amber-500"
          bg="bg-amber-500/10"
        />
        <BucketCard
          title="Всего"
          value={data.totalBase}
          currency={data.baseCurrency}
          icon={<TrendingUp className="size-4" />}
          color="text-primary"
          bg="bg-primary/10"
        />
      </div>

      {/* Token breakdown table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base font-medium">По токенам</CardTitle>
        </CardHeader>
        <CardContent>
          {data.byToken.length === 0 ? (
            <p className="text-sm text-muted-foreground py-6 text-center">
              Нет движений
            </p>
          ) : (
            <TooltipProvider>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Токен</TableHead>
                    <TableHead className="text-right">Доступно</TableHead>
                    <TableHead className="text-right">В ожидании</TableHead>
                    <TableHead className="text-right">Всего</TableHead>
                    <TableHead className="text-right">
                      Эквивалент ({data.baseCurrency?.toUpperCase() || "USD"})
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.byToken.map((token) => (
                    <TokenRow
                      key={token.tokenId}
                      token={token}
                      baseCurrency={data.baseCurrency}
                    />
                  ))}
                </TableBody>
              </Table>
            </TooltipProvider>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// ── Bucket Card ─────────────────────────────────────────────────────────

interface BucketCardProps {
  title: string
  value: string
  currency: string
  icon: React.ReactNode
  color: string
  bg: string
}

function BucketCard({ title, value, currency, icon, color, bg }: BucketCardProps) {
  const formatted = formatFiat(value)

  return (
    <Card>
      <CardContent className="pt-6">
        <div className="flex items-center gap-3">
          <div className={`size-9 rounded-lg ${bg} flex items-center justify-center ${color}`}>
            {icon}
          </div>
          <div>
            <p className="text-xs font-medium text-muted-foreground">{title}</p>
            <div className="flex items-baseline gap-1.5">
              <span className="text-2xl font-bold tracking-tight tabular-nums">
                {formatted}
              </span>
              {currency && (
                <span className="text-xs font-medium text-muted-foreground">
                  {currency.toUpperCase()}
                </span>
              )}
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

// ── Token Row ───────────────────────────────────────────────────────────

interface TokenRowProps {
  token: PortalTokenDetailed
  baseCurrency: string
}

function TokenRow({ token, baseCurrency }: TokenRowProps) {
  return (
    <TableRow>
      {/* Token */}
      <TableCell>
        <div className="flex items-center gap-2">
          <div className="size-7 rounded-full bg-primary/10 flex items-center justify-center">
            <TrendingUp className="size-3.5 text-primary" />
          </div>
          <div>
            <span className="font-medium text-sm">{token.tokenSymbol}</span>
            {token.currencyCode &&
              token.currencyCode !== token.tokenSymbol && (
                <p className="text-[10px] text-muted-foreground">
                  {token.currencyCode}
                </p>
              )}
          </div>
        </div>
      </TableCell>

      {/* Available */}
      <TableCell className="text-right">
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="tabular-nums text-sm font-medium text-emerald-600 dark:text-emerald-400 cursor-default">
              {formatCrypto(token.availableHuman)}
            </span>
          </TooltipTrigger>
          <TooltipContent side="left" className="text-xs">
            Minor units: {token.availableRaw}
          </TooltipContent>
        </Tooltip>
      </TableCell>

      {/* Pending */}
      <TableCell className="text-right">
        {token.pendingRaw !== "0" ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="tabular-nums text-sm font-medium text-amber-600 dark:text-amber-400 cursor-default">
                {formatCrypto(token.pendingHuman)}
              </span>
            </TooltipTrigger>
            <TooltipContent side="left" className="text-xs">
              Minor units: {token.pendingRaw}
            </TooltipContent>
          </Tooltip>
        ) : (
          <span className="text-sm text-muted-foreground">—</span>
        )}
      </TableCell>

      {/* Total */}
      <TableCell className="text-right">
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="tabular-nums text-sm font-semibold cursor-default">
              {formatCrypto(token.humanAmount)}
            </span>
          </TooltipTrigger>
          <TooltipContent side="left" className="text-xs">
            Minor units: {token.rawAmount}
          </TooltipContent>
        </Tooltip>
      </TableCell>

      {/* Fiat equivalent */}
      <TableCell className="text-right">
        {token.hasRate ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="tabular-nums text-sm text-muted-foreground cursor-default">
                ≈ {formatFiat(token.baseAmount)}{" "}
                <span className="text-[10px]">
                  {baseCurrency?.toUpperCase()}
                </span>
              </span>
            </TooltipTrigger>
            <TooltipContent side="left" className="text-xs">
              Курс: {parseFloat(token.rate).toFixed(6)} × {token.multiplier}
            </TooltipContent>
          </Tooltip>
        ) : (
          <Badge variant="outline" className="text-[10px] text-amber-500">
            нет курса
          </Badge>
        )}
      </TableCell>
    </TableRow>
  )
}

// ── Formatters ──────────────────────────────────────────────────────────

function formatFiat(value: string): string {
  const num = parseFloat(value)
  if (isNaN(num)) return "0.00"
  try {
    return num.toLocaleString("ru-RU", {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    })
  } catch {
    return num.toFixed(2)
  }
}

function formatCrypto(value: string): string {
  const num = parseFloat(value)
  if (isNaN(num)) return "0"
  return num.toLocaleString("ru-RU", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 8,
  })
}

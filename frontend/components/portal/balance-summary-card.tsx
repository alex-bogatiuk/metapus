"use client"

import { useEffect, useState } from "react"
import { TrendingUp, TrendingDown, Minus, Loader2, FileText, CircleDollarSign, Clock } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import type { PortalSummaryResponse } from "@/types/portal-api"

interface BalanceSummaryCardProps {
  merchantId: string | null
}

export function BalanceSummaryCard({ merchantId }: BalanceSummaryCardProps) {
  const [data, setData] = useState<PortalSummaryResponse | null>(null)
  const [loading, setLoading] = useState(true)

  // Reset loading when merchantId changes (render-time state adjustment)
  const [prevMerchantId, setPrevMerchantId] = useState(merchantId)
  if (merchantId !== prevMerchantId) {
    setPrevMerchantId(merchantId)
    setLoading(true)
  }

  useEffect(() => {
    api.portal.summary(merchantId ?? undefined)
      .then(setData)
      .catch(() => setData(null))
      .finally(() => setLoading(false))
  }, [merchantId])

  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center h-40">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    )
  }

  if (!data) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center h-40 text-muted-foreground text-sm">
          Нет данных
        </CardContent>
      </Card>
    )
  }

  const changeNum = parseFloat(data.change24hPct)
  const isPositive = changeNum > 0
  const isNegative = changeNum < 0
  const TrendIcon = isPositive ? TrendingUp : isNegative ? TrendingDown : Minus

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          Общий баланс
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Total received in minor units — display raw for now */}
        <div className="flex items-baseline gap-2">
          <span className="text-3xl font-bold tracking-tight tabular-nums">
            {formatMinorUnits(data.totalMinorUnits)}
          </span>
          <div className={`flex items-center gap-1 text-xs font-medium ${
            isPositive ? "text-emerald-500" : isNegative ? "text-red-500" : "text-muted-foreground"
          }`}>
            <TrendIcon className="size-3" />
            <span>{data.change24hPct}%</span>
          </div>
        </div>

        {/* Stats grid */}
        <div className="grid grid-cols-3 gap-4">
          <div className="flex items-center gap-2">
            <FileText className="size-4 text-muted-foreground" />
            <div>
              <p className="text-lg font-semibold tabular-nums">{data.totalInvoices}</p>
              <p className="text-[10px] text-muted-foreground">Всего</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <CircleDollarSign className="size-4 text-emerald-500" />
            <div>
              <p className="text-lg font-semibold tabular-nums">{data.paidInvoices}</p>
              <p className="text-[10px] text-muted-foreground">Оплачено</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Clock className="size-4 text-amber-500" />
            <div>
              <p className="text-lg font-semibold tabular-nums">{data.pendingInvoices}</p>
              <p className="text-[10px] text-muted-foreground">В ожидании</p>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

/** Format minor units to a readable number with grouping. */
function formatMinorUnits(val: string): string {
  const n = BigInt(val)
  return n.toLocaleString("ru-RU")
}

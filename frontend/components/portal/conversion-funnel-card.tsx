"use client"

import { useEffect, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"
import type { PortalFunnelResponse } from "@/types/portal-api"
import { ArrowRight, TrendingUp, AlertTriangle } from "lucide-react"

interface ConversionFunnelCardProps {
  merchantId?: string
}

interface FunnelStep {
  label: string
  value: number
  color: string
  bgColor: string
}

export function ConversionFunnelCard({ merchantId }: ConversionFunnelCardProps) {
  const [data, setData] = useState<PortalFunnelResponse | null>(null)
  const [loading, setLoading] = useState(true)

  // Reset loading when merchantId changes (render-time state adjustment)
  const [prevMerchantId, setPrevMerchantId] = useState(merchantId)
  if (merchantId !== prevMerchantId) {
    setPrevMerchantId(merchantId)
    setLoading(true)
  }

  useEffect(() => {
    api.portal
      .funnel(merchantId)
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [merchantId])

  if (loading) {
    return (
      <Card>
        <CardHeader className="pb-2">
          <Skeleton className="h-5 w-40" />
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-16 flex-1 rounded-lg" />
            ))}
          </div>
        </CardContent>
      </Card>
    )
  }

  if (!data || data.total === 0) {
    return (
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            Воронка конверсии
          </CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">Нет данных для отображения</p>
        </CardContent>
      </Card>
    )
  }

  const pct = (n: number) => data.total > 0 ? ((n / data.total) * 100).toFixed(1) : "0"
  const expiredPct = parseFloat(pct(data.expired))

  const steps: FunnelStep[] = [
    {
      label: "Создано",
      value: data.total,
      color: "text-blue-700 dark:text-blue-400",
      bgColor: "bg-blue-50 dark:bg-blue-950/40",
    },
    {
      label: "Получен платёж",
      value: data.receivedAny,
      color: "text-amber-700 dark:text-amber-400",
      bgColor: "bg-amber-50 dark:bg-amber-950/40",
    },
    {
      label: "Полная оплата",
      value: data.fullyPaid,
      color: "text-emerald-700 dark:text-emerald-400",
      bgColor: "bg-emerald-50 dark:bg-emerald-950/40",
    },
    {
      label: "Подтверждено",
      value: data.confirmed,
      color: "text-violet-700 dark:text-violet-400",
      bgColor: "bg-violet-50 dark:bg-violet-950/40",
    },
  ]

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-1.5">
            <TrendingUp className="size-4" />
            Воронка конверсии
          </CardTitle>
          {expiredPct > 30 && (
            <span className="flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400">
              <AlertTriangle className="size-3" />
              {pct(data.expired)}% expired
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex items-stretch gap-1">
          {steps.map((step, i) => (
            <div key={step.label} className="flex items-center gap-1 flex-1 min-w-0">
              <div
                className={`flex-1 rounded-lg p-3 ${step.bgColor} transition-colors`}
              >
                <p className="text-[11px] font-medium text-muted-foreground truncate">
                  {step.label}
                </p>
                <p className={`text-lg font-bold tabular-nums ${step.color}`}>
                  {step.value.toLocaleString()}
                </p>
                <p className="text-[10px] text-muted-foreground">
                  {pct(step.value)}%
                </p>
              </div>
              {i < steps.length - 1 && (
                <ArrowRight className="size-3.5 shrink-0 text-muted-foreground/40" />
              )}
            </div>
          ))}
        </div>

        {/* Expired bar at bottom */}
        {data.expired > 0 && (
          <div className="mt-3 flex items-center gap-2 text-xs">
            <div className="flex-1 h-1.5 bg-muted rounded-full overflow-hidden">
              <div
                className="h-full bg-red-400 dark:bg-red-500 rounded-full transition-all"
                style={{ width: `${Math.min(parseFloat(pct(data.expired)), 100)}%` }}
              />
            </div>
            <span className="text-muted-foreground tabular-nums whitespace-nowrap">
              {data.expired.toLocaleString()} expired ({pct(data.expired)}%)
            </span>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

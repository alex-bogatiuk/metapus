"use client"

import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { api } from "@/lib/api"
import type { PortalCurrencyItem } from "@/types/portal-api"

interface CurrencyBreakdownCardProps {
  merchantId: string | null
}

export function CurrencyBreakdownCard({ merchantId }: CurrencyBreakdownCardProps) {
  const [items, setItems] = useState<PortalCurrencyItem[]>([])
  const [loading, setLoading] = useState(true)

  // Reset loading when merchantId changes (render-time state adjustment)
  const [prevMerchantId, setPrevMerchantId] = useState(merchantId)
  if (merchantId !== prevMerchantId) {
    setPrevMerchantId(merchantId)
    setLoading(true)
  }

  useEffect(() => {
    api.portal.currencies(merchantId ?? undefined)
      .then((res) => setItems(res.items))
      .catch(() => setItems([]))
      .finally(() => setLoading(false))
  }, [merchantId])

  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center h-52">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          По валютам
        </CardTitle>
      </CardHeader>
      <CardContent>
        {items.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4 text-center">
            Нет данных
          </p>
        ) : (
          <div className="space-y-3">
            {items.map((item) => (
              <div key={`${item.symbol}-${item.network}`} className="space-y-1.5">
                <div className="flex items-center justify-between text-sm">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{item.symbol}</span>
                    <span className="text-[10px] text-muted-foreground px-1.5 py-0.5 rounded bg-muted">
                      {item.network}
                    </span>
                  </div>
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {formatWithDecimals(item.totalMinor, item.decimalPlaces)}
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <Progress
                    value={parseFloat(item.sharePct)}
                    className="h-1.5 flex-1"
                  />
                  <span className="text-[10px] tabular-nums text-muted-foreground w-10 text-right">
                    {item.sharePct}%
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

/** Format minor units to major units with correct decimal places. */
function formatWithDecimals(minor: string, decimals: number): string {
  if (decimals === 0) return BigInt(minor).toLocaleString("ru-RU")
  const n = BigInt(minor)
  const divisor = BigInt(10 ** decimals)
  const whole = n / divisor
  const frac = (n % divisor).toString().padStart(decimals, "0")
  return `${whole.toLocaleString("ru-RU")}.${frac}`
}

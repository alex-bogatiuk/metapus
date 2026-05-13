"use client"

import { useEffect, useState } from "react"
import { Loader2, Wallet, AlertCircle, TrendingUp } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import type { PortalBalanceResponse, PortalTokenBalance } from "@/types/portal-api"

interface MerchantBalanceCardProps {
  merchantId: string | null
}

export function MerchantBalanceCard({ merchantId }: MerchantBalanceCardProps) {
  const [data, setData] = useState<PortalBalanceResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  // Reset loading when merchantId changes (render-time state adjustment)
  const [prevMerchantId, setPrevMerchantId] = useState(merchantId)
  if (merchantId !== prevMerchantId) {
    setPrevMerchantId(merchantId)
    setLoading(true)
    setError(false)
  }

  useEffect(() => {
    api.portal.balance(merchantId ?? undefined)
      .then((res) => {
        setData(res)
        setError(false)
      })
      .catch(() => {
        setData(null)
        setError(true)
      })
      .finally(() => setLoading(false))
  }, [merchantId])

  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center h-48">
          <Loader2 className="size-5 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    )
  }

  if (error || !data) {
    return (
      <Card>
        <CardContent className="flex flex-col items-center justify-center h-48 gap-2 text-muted-foreground text-sm">
          <AlertCircle className="size-5" />
          <span>Не удалось загрузить баланс</span>
        </CardContent>
      </Card>
    )
  }

  const hasAnyRate = data.byToken.some((t) => t.hasRate)
  const totalFormatted = formatFiat(data.totalBase)

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium text-muted-foreground flex items-center gap-2">
            <Wallet className="size-4" />
            Баланс мерчанта
          </CardTitle>
          {data.rateSource && (
            <Badge variant="outline" className="text-[10px] font-normal capitalize">
              {data.rateSource}
            </Badge>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Total fiat balance */}
        <div className="flex items-baseline gap-2">
          <span className="text-3xl font-bold tracking-tight tabular-nums">
            {totalFormatted}
          </span>
          {data.baseCurrency && (
            <span className="text-sm font-medium text-muted-foreground">
              {data.baseCurrency.toUpperCase()}
            </span>
          )}
        </div>

        {/* Token breakdown */}
        {data.byToken.length > 0 ? (
          <div className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">По токенам</p>
            <TooltipProvider>
              <div className="space-y-1.5">
                {data.byToken.map((token) => (
                  <TokenRow key={token.tokenId} token={token} baseCurrency={data.baseCurrency} />
                ))}
              </div>
            </TooltipProvider>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">Нет движений</p>
        )}

        {/* Warning if some tokens have no rate */}
        {data.byToken.length > 0 && !hasAnyRate && (
          <div className="flex items-center gap-1.5 text-xs text-amber-500">
            <AlertCircle className="size-3" />
            <span>Курсы обмена не настроены</span>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

interface TokenRowProps {
  token: PortalTokenBalance
  baseCurrency: string
}

function TokenRow({ token, baseCurrency }: TokenRowProps) {
  const humanAmount = formatCrypto(token.humanAmount)
  const fiatAmount = token.hasRate ? formatFiat(token.baseAmount) : "—"

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-center justify-between py-1 px-2 rounded-md hover:bg-muted/50 transition-colors cursor-default">
          <div className="flex items-center gap-2">
            <div className="size-6 rounded-full bg-primary/10 flex items-center justify-center">
              <TrendingUp className="size-3 text-primary" />
            </div>
            <div>
              <p className="text-sm font-medium">{token.tokenSymbol}</p>
              {token.currencyCode && token.currencyCode !== token.tokenSymbol && (
                <p className="text-[10px] text-muted-foreground">{token.currencyCode}</p>
              )}
            </div>
          </div>
          <div className="text-right">
            <p className="text-sm font-semibold tabular-nums">{humanAmount}</p>
            {token.hasRate && (
              <p className="text-[10px] text-muted-foreground tabular-nums">
                ≈ {fiatAmount}
              </p>
            )}
            {!token.hasRate && (
              <p className="text-[10px] text-amber-500">нет курса</p>
            )}
          </div>
        </div>
      </TooltipTrigger>
      <TooltipContent side="left" className="text-xs space-y-1">
        <p>Minor units: {token.rawAmount}</p>
        {token.hasRate && (
          <>
            <p>Курс: {parseFloat(token.rate).toFixed(6)} × {token.multiplier}</p>
            <p>В {baseCurrency.toUpperCase()}: {token.baseAmount}</p>
          </>
        )}
      </TooltipContent>
    </Tooltip>
  )
}

/** Format a decimal string as a fiat currency display. */
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

/** Format a decimal string as a crypto amount (trim trailing zeros). */
function formatCrypto(value: string): string {
  const num = parseFloat(value)
  if (isNaN(num)) return "0"
  // Show up to 8 decimals, trim trailing zeros.
  return num.toLocaleString("ru-RU", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 8,
  })
}

"use client"

import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import type { PortalInvoiceItem } from "@/types/portal-api"

interface RecentInvoicesCardProps {
  merchantId: string | null
}

const STATUS_LABELS: Record<string, string> = {
  created: "Создан",
  partially_paid: "Частично",
  confirmed: "Подтверждён",
  expired: "Истёк",
  cancelled: "Отменён",
}

const STATUS_VARIANTS: Record<string, "default" | "secondary" | "destructive" | "outline"> = {
  created: "outline",
  partially_paid: "secondary",
  confirmed: "default",
  expired: "destructive",
  cancelled: "destructive",
}

type TabFilter = "" | "confirmed" | "created" | "expired"

export function RecentInvoicesCard({ merchantId }: RecentInvoicesCardProps) {
  const [filter, setFilter] = useState<TabFilter>("")
  const [items, setItems] = useState<PortalInvoiceItem[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)

  // Reset loading when deps change (render-time state adjustment)
  const fetchKey = `${merchantId}-${filter}`
  const [prevFetchKey, setPrevFetchKey] = useState(fetchKey)
  if (fetchKey !== prevFetchKey) {
    setPrevFetchKey(fetchKey)
    setLoading(true)
  }

  useEffect(() => {
    api.portal.invoices({
      merchantId: merchantId ?? undefined,
      status: filter || undefined,
      limit: 10,
    })
      .then((res) => {
        setItems(res.items)
        setTotal(res.total)
      })
      .catch(() => {
        setItems([])
        setTotal(0)
      })
      .finally(() => setLoading(false))
  }, [merchantId, filter])

  return (
    <Card>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            Инвойсы
            {total > 0 && (
              <span className="ml-1.5 text-[10px] text-muted-foreground">
                ({total})
              </span>
            )}
          </CardTitle>
          <Tabs value={filter} onValueChange={(v) => setFilter(v as TabFilter)}>
            <TabsList className="h-7">
              <TabsTrigger value="" className="text-xs px-2 h-5">Все</TabsTrigger>
              <TabsTrigger value="confirmed" className="text-xs px-2 h-5">Оплачен</TabsTrigger>
              <TabsTrigger value="created" className="text-xs px-2 h-5">Ожидает</TabsTrigger>
              <TabsTrigger value="expired" className="text-xs px-2 h-5">Истёк</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>
      </CardHeader>
      <CardContent>
        {loading ? (
          <div className="flex items-center justify-center h-40">
            <Loader2 className="size-5 animate-spin text-muted-foreground" />
          </div>
        ) : items.length === 0 ? (
          <p className="text-sm text-muted-foreground py-8 text-center">
            Нет инвойсов
          </p>
        ) : (
          <div className="space-y-1">
            {items.map((inv) => (
              <div
                key={inv.id}
                className="flex items-center gap-3 py-2 px-2 rounded-md hover:bg-muted/50 transition-colors"
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-mono">#{inv.number}</span>
                    <Badge variant={STATUS_VARIANTS[inv.status] ?? "secondary"} className="text-[10px] px-1.5 py-0">
                      {STATUS_LABELS[inv.status] ?? inv.status}
                    </Badge>
                  </div>
                  <p className="text-[10px] text-muted-foreground mt-0.5">
                    {inv.symbol} · {inv.network} · {formatDate(inv.createdAt)}
                  </p>
                </div>
                <div className="text-right shrink-0">
                  <p className="text-sm font-medium tabular-nums">
                    {formatWithDecimals(inv.receivedAmount, inv.decimalPlaces)}
                  </p>
                  <p className="text-[10px] text-muted-foreground tabular-nums">
                    / {formatWithDecimals(inv.amount, inv.decimalPlaces)}
                  </p>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function formatDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString("ru-RU", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

function formatWithDecimals(minor: string, decimals: number): string {
  if (decimals === 0) return BigInt(minor).toLocaleString("ru-RU")
  const n = BigInt(minor)
  const divisor = BigInt(10 ** decimals)
  const whole = n / divisor
  const frac = (n % divisor).toString().padStart(decimals, "0")
  return `${whole.toLocaleString("ru-RU")}.${frac}`
}

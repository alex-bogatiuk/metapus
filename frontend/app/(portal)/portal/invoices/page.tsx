"use client"

import { useEffect, useState } from "react"
import { Loader2 } from "lucide-react"

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
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { usePortalStore } from "@/stores/usePortalStore"
import type { PortalInvoiceItem } from "@/types/portal-api"

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

const PAGE_SIZE = 20

export default function PortalInvoicesPage() {
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  const [items, setItems] = useState<PortalInvoiceItem[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setLoading(true)
    setOffset(0) // reset when merchant changes
  }, [activeMerchantId])

  useEffect(() => {
    setLoading(true)
    api.portal.invoices({
      merchantId: activeMerchantId ?? undefined,
      limit: PAGE_SIZE,
      offset,
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
  }, [activeMerchantId, offset])

  const totalPages = Math.ceil(total / PAGE_SIZE)
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Инвойсы</h1>
        <p className="text-sm text-muted-foreground">
          Все платёжные инвойсы мерчанта
        </p>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {total > 0 && `${total} инвойс(ов)`}
          </CardTitle>
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
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Номер</TableHead>
                    <TableHead>Статус</TableHead>
                    <TableHead>Токен</TableHead>
                    <TableHead className="text-right">Сумма</TableHead>
                    <TableHead className="text-right">Получено</TableHead>
                    <TableHead>Дата</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((inv) => (
                    <TableRow key={inv.id}>
                      <TableCell className="font-mono text-sm">
                        #{inv.number}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={STATUS_VARIANTS[inv.status] ?? "secondary"}
                          className="text-[10px]"
                        >
                          {STATUS_LABELS[inv.status] ?? inv.status}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm">{inv.symbol}</span>
                        <span className="text-[10px] text-muted-foreground ml-1">
                          {inv.network}
                        </span>
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-sm">
                        {formatWithDecimals(inv.amount, inv.decimalPlaces)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums text-sm">
                        {formatWithDecimals(inv.receivedAmount, inv.decimalPlaces)}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDate(inv.createdAt)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {/* Pagination */}
              {totalPages > 1 && (
                <div className="flex items-center justify-between pt-4">
                  <p className="text-xs text-muted-foreground">
                    Страница {currentPage} из {totalPages}
                  </p>
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={offset === 0}
                      onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                    >
                      Назад
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={offset + PAGE_SIZE >= total}
                      onClick={() => setOffset(offset + PAGE_SIZE)}
                    >
                      Далее
                    </Button>
                  </div>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

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
  if (decimals === 0) return BigInt(minor).toLocaleString("ru-RU")
  const n = BigInt(minor)
  const divisor = BigInt(10 ** decimals)
  const whole = n / divisor
  const frac = (n % divisor).toString().padStart(decimals, "0")
  return `${whole.toLocaleString("ru-RU")}.${frac}`
}

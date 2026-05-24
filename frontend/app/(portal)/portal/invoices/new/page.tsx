"use client"

import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import type { PortalCurrencyItem, PortalCreateInvoiceRequest } from "@/types/portal-api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { ArrowLeft, Loader2, Plus, Receipt } from "lucide-react"
import { toast } from "sonner"
import Link from "next/link"

const TTL_OPTIONS = [
  { value: "15", label: "15 минут" },
  { value: "30", label: "30 минут" },
  { value: "60", label: "1 час" },
  { value: "120", label: "2 часа" },
  { value: "360", label: "6 часов" },
  { value: "720", label: "12 часов" },
  { value: "1440", label: "24 часа" },
]

export default function CreateInvoicePage() {
  const router = useRouter()
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)

  // Token list (from currencies endpoint)
  const [tokens, setTokens] = useState<PortalCurrencyItem[]>([])
  const [tokensLoading, setTokensLoading] = useState(true)

  // Form state
  const [selectedTokenId, setSelectedTokenId] = useState<string>("")
  const [amount, setAmount] = useState("")
  const [ttl, setTtl] = useState("60")
  const [description, setDescription] = useState("")
  const [orderId, setOrderId] = useState("")
  const [customerEmail, setCustomerEmail] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const fetchTokens = useCallback(async () => {
    if (!activeMerchantId) return
    setTokensLoading(true)
    try {
      const resp = await api.portal.currencies(activeMerchantId)
      const data = resp.items
      setTokens(data)
      if (data.length > 0 && !selectedTokenId) {
        setSelectedTokenId(data[0].tokenId)
      }
    } catch {
      toast.error("Не удалось загрузить список токенов")
    } finally {
      setTokensLoading(false)
    }
  }, [activeMerchantId, selectedTokenId])

  useEffect(() => {
    fetchTokens()
  }, [fetchTokens])

  const activeToken = tokens.find((t) => t.tokenId === selectedTokenId)

  const handleSubmit = async () => {
    if (!activeMerchantId || !activeToken || !amount) return

    setSubmitting(true)
    try {
      const body: PortalCreateInvoiceRequest = {
        tokenId: activeToken.tokenId,
        amount,
        ttlMinutes: parseInt(ttl),
        description: description || undefined,
        orderId: orderId || undefined,
        customerEmail: customerEmail || undefined,
      }

      const resp = await api.portal.createInvoice(activeMerchantId, body)
      toast.success(`Инвойс ${resp.number} создан`)
      router.push(`/portal/invoices/${resp.id}`)
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Ошибка создания инвойса"
      toast.error(msg)
    } finally {
      setSubmitting(false)
    }
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Receipt className="size-10 mb-3 opacity-40" />
        <p>Выберите мерчанта</p>
      </div>
    )
  }

  return (
    <div className="space-y-6 max-w-2xl">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" asChild>
          <Link href="/portal/invoices">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Создать инвойс</h1>
          <p className="text-sm text-muted-foreground">
            Создайте платёжный запрос для получения криптовалюты
          </p>
        </div>
      </div>

      <div className="grid gap-6 md:grid-cols-5">
        {/* Form */}
        <Card className="md:col-span-3">
          <CardHeader>
            <CardTitle className="text-base">Параметры инвойса</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {tokensLoading ? (
              <div className="space-y-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : (
              <>
                {/* Token */}
                <div className="space-y-2">
                  <Label>Токен</Label>
                  <Select value={selectedTokenId} onValueChange={setSelectedTokenId}>
                    <SelectTrigger>
                      <SelectValue placeholder="Выберите токен" />
                    </SelectTrigger>
                    <SelectContent>
                      {tokens.map((t) => (
                        <SelectItem
                          key={t.tokenId}
                          value={t.tokenId}
                        >
                          {t.symbol}
                          <span className="text-muted-foreground ml-1.5 text-xs">
                            {t.network}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                {/* Amount */}
                <div className="space-y-2">
                  <Label>Сумма</Label>
                  <div className="relative">
                    <Input
                      type="number"
                      step={
                        activeToken
                          ? (1 / Math.pow(10, activeToken.decimalPlaces)).toFixed(
                              activeToken.decimalPlaces
                            )
                          : "0.01"
                      }
                      min="0"
                      placeholder="0.00"
                      value={amount}
                      onChange={(e) => setAmount(e.target.value)}
                      className="pr-16"
                    />
                    {activeToken && (
                      <span className="absolute right-3 top-1/2 -translate-y-1/2 text-sm font-medium text-muted-foreground">
                        {activeToken.symbol}
                      </span>
                    )}
                  </div>
                  {activeToken && (
                    <p className="text-[11px] text-muted-foreground">
                      Точность: {activeToken.decimalPlaces} знаков после запятой
                    </p>
                  )}
                </div>

                {/* TTL */}
                <div className="space-y-2">
                  <Label>Время жизни</Label>
                  <Select value={ttl} onValueChange={setTtl}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {TTL_OPTIONS.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {opt.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-[11px] text-muted-foreground">
                    После истечения инвойс автоматически отменится
                  </p>
                </div>

                <Separator />

                {/* Description */}
                <div className="space-y-2">
                  <Label>
                    Описание <span className="text-muted-foreground">(необязательно)</span>
                  </Label>
                  <Textarea
                    placeholder="Оплата заказа #123"
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    rows={2}
                  />
                </div>

                {/* Order ID */}
                <div className="space-y-2">
                  <Label>
                    Order ID <span className="text-muted-foreground">(необязательно)</span>
                  </Label>
                  <Input
                    placeholder="order-123"
                    value={orderId}
                    onChange={(e) => setOrderId(e.target.value)}
                  />
                  <p className="text-[11px] text-muted-foreground">
                    Ключ идемпотентности — повторный запрос с тем же Order ID вернёт существующий инвойс
                  </p>
                </div>

                {/* Customer Email */}
                <div className="space-y-2">
                  <Label>
                    Email клиента <span className="text-muted-foreground">(необязательно)</span>
                  </Label>
                  <Input
                    type="email"
                    placeholder="customer@example.com"
                    value={customerEmail}
                    onChange={(e) => setCustomerEmail(e.target.value)}
                  />
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* Preview */}
        <Card className="md:col-span-2 h-fit">
          <CardHeader>
            <CardTitle className="text-base">Предпросмотр</CardTitle>
            <CardDescription>Инвойс будет создан с этими параметрами</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Токен</span>
              <span className="font-medium">
                {activeToken ? `${activeToken.symbol} (${activeToken.network})` : "—"}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Сумма</span>
              <span className="font-mono font-medium tabular-nums">
                {amount || "0.00"} {activeToken?.symbol ?? ""}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">TTL</span>
              <span>{TTL_OPTIONS.find((o) => o.value === ttl)?.label ?? ttl}</span>
            </div>
            {description && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Описание</span>
                <span className="text-right max-w-[160px] truncate">{description}</span>
              </div>
            )}
            {orderId && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Order ID</span>
                <span className="font-mono text-xs">{orderId}</span>
              </div>
            )}

            <Separator />

            <Button
              className="w-full"
              onClick={handleSubmit}
              disabled={submitting || !amount || !activeToken}
            >
              {submitting ? (
                <Loader2 className="size-4 mr-1.5 animate-spin" />
              ) : (
                <Plus className="size-4 mr-1.5" />
              )}
              Создать инвойс
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

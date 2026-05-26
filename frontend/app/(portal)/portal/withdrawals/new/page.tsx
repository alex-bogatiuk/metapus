"use client"

import { useCallback, useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import Link from "next/link"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import type {
  PortalWithdrawalAddress,
  PortalCreateWithdrawalRequest,
  PortalTokenBalance,
  PortalCurrencyItem,
} from "@/types/portal-api"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Badge } from "@/components/ui/badge"
import { ArrowLeft, Loader2, Send, Shield, Wallet } from "lucide-react"
import { toast } from "sonner"

export default function NewWithdrawalRequestPage() {
  const router = useRouter()
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)

  // Data
  const [addresses, setAddresses] = useState<PortalWithdrawalAddress[]>([])
  const [balances, setBalances] = useState<PortalTokenBalance[]>([])
  const [currencies, setCurrencies] = useState<PortalCurrencyItem[]>([])
  const [loading, setLoading] = useState(true)

  // Form: token first, then address (filtered by network)
  const [selectedTokenId, setSelectedTokenId] = useState("")
  const [selectedAddressId, setSelectedAddressId] = useState("")
  const [amount, setAmount] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const fetchData = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    try {
      const [addrRes, balRes, currRes] = await Promise.all([
        api.portal.withdrawalAddresses(activeMerchantId),
        api.portal.balance(activeMerchantId),
        api.portal.currencies(activeMerchantId),
      ])
      setAddresses(addrRes.items)
      setBalances(balRes.byToken)
      setCurrencies(currRes.items)
    } catch {
      toast.error("Не удалось загрузить данные")
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  // Selected token → resolve its networkId via currencies
  const selectedCurrency = currencies.find((c) => c.tokenId === selectedTokenId)
  const selectedAddress = addresses.find((a) => a.id === selectedAddressId)

  // Filter addresses to only show those matching the selected token's network
  const filteredAddresses = selectedCurrency
    ? addresses.filter((a) => a.networkId === selectedCurrency.networkId)
    : []

  // Reset address when token changes and current address doesn't match
  const tokenBalance = selectedTokenId
    ? balances.find((b) => b.tokenId === selectedTokenId)
    : undefined

  const handleSubmit = async () => {
    if (!activeMerchantId || !selectedAddress || !amount) return

    setSubmitting(true)
    try {
      const body: PortalCreateWithdrawalRequest = {
        tokenId: selectedTokenId,
        amount,
        addressId: selectedAddress.id,
      }

      const resp = await api.portal.createWithdrawalRequest(activeMerchantId, body)
      toast.success(`Заявка ${resp.number} создана`)
      router.push("/portal/withdrawals")
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Ошибка создания заявки"
      toast.error(msg)
    } finally {
      setSubmitting(false)
    }
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Wallet className="size-10 mb-3 opacity-40" />
        <p>Выберите мерчанта</p>
      </div>
    )
  }

  return (
    <div className="space-y-6 max-w-2xl">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" asChild>
          <Link href="/portal/withdrawals">
            <ArrowLeft className="size-4" />
          </Link>
        </Button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Запрос вывода</h1>
          <p className="text-sm text-muted-foreground">
            Создайте заявку на вывод средств. Заявка будет отправлена на одобрение.
          </p>
        </div>
      </div>

      <div className="grid gap-6 md:grid-cols-5">
        {/* Form */}
        <Card className="md:col-span-3">
          <CardHeader>
            <CardTitle className="text-base">Параметры вывода</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {loading ? (
              <div className="space-y-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : addresses.length === 0 ? (
              <div className="text-center py-8 space-y-3">
                <Shield className="size-8 mx-auto text-muted-foreground opacity-50" />
                <p className="text-sm text-muted-foreground">
                  Нет адресов в белом списке.
                </p>
                <Button variant="outline" size="sm" asChild>
                  <Link href="/portal/withdrawals/addresses">
                    Добавить адрес
                  </Link>
                </Button>
              </div>
            ) : (
              <>
                {/* Token */}
                <div className="space-y-2">
                  <Label>Токен</Label>
                  <Select value={selectedTokenId} onValueChange={(v) => {
                    setSelectedTokenId(v)
                    setSelectedAddressId("") // reset address when token changes
                  }}>
                    <SelectTrigger>
                      <SelectValue placeholder="Выберите токен" />
                    </SelectTrigger>
                    <SelectContent>
                      {balances.map((b) => (
                        <SelectItem key={b.tokenId} value={b.tokenId}>
                          <div className="flex items-center gap-2">
                            <span>{b.tokenSymbol}</span>
                            <span className="text-muted-foreground text-xs">
                              {b.humanAmount}
                            </span>
                          </div>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                {/* Address (filtered by token's network) */}
                <div className="space-y-2">
                  <Label>Адрес назначения</Label>
                  <Select
                    value={selectedAddressId}
                    onValueChange={setSelectedAddressId}
                    disabled={!selectedTokenId}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder={selectedTokenId ? "Выберите адрес" : "Сначала выберите токен"} />
                    </SelectTrigger>
                    <SelectContent>
                      {filteredAddresses.length === 0 && selectedTokenId ? (
                        <div className="px-2 py-3 text-sm text-muted-foreground text-center">
                          Нет адресов для этой сети
                        </div>
                      ) : (
                        filteredAddresses.map((a) => (
                          <SelectItem key={a.id} value={a.id}>
                            <div className="flex items-center gap-2">
                              <span className="font-mono text-xs">
                                {a.address.slice(0, 8)}...{a.address.slice(-6)}
                              </span>
                              <Badge variant="outline" className="text-[10px]">
                                {a.network}
                              </Badge>
                              {a.label && (
                                <span className="text-muted-foreground text-xs">
                                  ({a.label})
                                </span>
                              )}
                            </div>
                          </SelectItem>
                        ))
                      )}
                    </SelectContent>
                  </Select>
                </div>

                {/* Amount */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <Label>Сумма</Label>
                    {tokenBalance && (
                      <button
                        type="button"
                        className="text-[11px] text-primary hover:underline"
                        onClick={() => setAmount(tokenBalance.humanAmount)}
                      >
                        Макс: {parseFloat(tokenBalance.humanAmount).toLocaleString("ru-RU", { maximumFractionDigits: 8 })} {selectedCurrency?.symbol}
                      </button>
                    )}
                  </div>
                  <div className="relative">
                    <Input
                      type="number"
                      step="0.000001"
                      min="0"
                      placeholder="0.00"
                      value={amount}
                      onChange={(e) => setAmount(e.target.value)}
                      className="pr-16"
                    />
                    {selectedCurrency && (
                      <span className="absolute right-3 top-1/2 -translate-y-1/2 text-sm font-medium text-muted-foreground">
                        {selectedCurrency.symbol}
                      </span>
                    )}
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* Preview */}
        <Card className="md:col-span-2 h-fit">
          <CardHeader>
            <CardTitle className="text-base">Сводка</CardTitle>
            <CardDescription>Заявка на вывод средств</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Токен</span>
              <span className="font-medium">
                {selectedCurrency ? `${selectedCurrency.symbol} (${selectedCurrency.network})` : "—"}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Сумма</span>
              <span className="font-mono font-medium tabular-nums">
                {amount || "0.00"} {selectedCurrency?.symbol ?? ""}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Адрес</span>
              <span className="font-mono text-xs max-w-[140px] truncate">
                {selectedAddress?.address ?? "—"}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Статус</span>
              <Badge variant="outline" className="text-amber-500 text-xs">
                Ожидает одобрения
              </Badge>
            </div>

            <Separator />

            <Button
              className="w-full"
              onClick={handleSubmit}
              disabled={submitting || !amount || !selectedAddress}
            >
              {submitting ? (
                <Loader2 className="size-4 mr-1.5 animate-spin" />
              ) : (
                <Send className="size-4 mr-1.5" />
              )}
              Отправить заявку
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

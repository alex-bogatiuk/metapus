"use client"

import { useCallback, useEffect, useState } from "react"
import { useRouter, useSearchParams } from "next/navigation"
import Link from "next/link"
import { usePortalStore } from "@/stores/usePortalStore"
import { api } from "@/lib/api"
import type {
  PortalWithdrawalAddress,
  PortalCreateWithdrawalRequest,
  PortalTokenBalance,
  PortalCurrencyItem,
  PortalRefundablePaymentResponse,
 WithdrawalRiskClass,
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
import { ArrowLeft, Copy, Loader2, Send, Shield, Wallet } from "lucide-react"
import { toast } from "sonner"
import { riskClassLabels as riskLabels, isRiskClass } from "@/lib/aml-risk"
import type { PortalRiskBalanceResponse } from "@/types/generated/http-contracts"
import { rawMinorToMajor } from "@/lib/money"
import { formatMinorUnits } from "@/lib/format"
import { Field, FieldGroup, FieldLabel, FieldDescription } from "@/components/ui/field"
import { Checkbox } from "@/components/ui/checkbox"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"



// "40.500000" → "40.5", "40.000000" → "40": the amount field takes it as typed.
function trimFraction(value: string): string {
  return value.includes(".") ? value.replace(/0+$/, "").replace(/\.$/, "") : value
}

export default function NewWithdrawalRequestPage() {
  const router = useRouter()
  const activeMerchantId = usePortalStore((s) => s.activeMerchantId)
  // ?refund=<paymentId> turns the request into a refund of that credited payment:
  // the token is fixed to the payment's and the amount is capped by what is left.
  const refundPaymentId = useSearchParams().get("refund")
  const [refund, setRefund] = useState<PortalRefundablePaymentResponse | null>(null)
  const [refundReason, setRefundReason] = useState("")

  // Data
  const [addresses, setAddresses] = useState<PortalWithdrawalAddress[]>([])
  const [balances, setBalances] = useState<PortalTokenBalance[]>([])
  const [currencies, setCurrencies] = useState<PortalCurrencyItem[]>([])
  const [loading, setLoading] = useState(true)
  const [riskBalance, setRiskBalance] = useState<PortalRiskBalanceResponse | null>(null)
  const [riskClass, setRiskClass] = useState<WithdrawalRiskClass>("low")
  const [riskAccepted, setRiskAccepted] = useState(false)

  // Form: token first, then address (filtered by network)
  const [selectedTokenId, setSelectedTokenId] = useState("")
  const [selectedAddressId, setSelectedAddressId] = useState("")
  const [amount, setAmount] = useState("")
  const [submitting, setSubmitting] = useState(false)

  const fetchData = useCallback(async () => {
    if (!activeMerchantId) return
    setLoading(true)
    setBalances([])
    setRiskBalance(null)
    setSelectedTokenId("")
    setSelectedAddressId("")
    setRiskAccepted(false)
    setRefund(null)
    try {
      const [addrRes, balRes, currRes, riskRes, refundRes] = await Promise.all([
        api.portal.withdrawalAddresses(activeMerchantId),
        api.portal.balance(activeMerchantId),
        api.portal.currencies(activeMerchantId),
        api.portal.riskBalance(activeMerchantId),
        refundPaymentId ? api.portal.refundablePayment(activeMerchantId, refundPaymentId) : Promise.resolve(null),
      ])
      setAddresses(addrRes.items)
      setBalances(balRes.byToken)
      setCurrencies(currRes.items)
      setRiskBalance(riskRes)
      setRefund(refundRes)
      if (refundRes) setSelectedTokenId(refundRes.tokenId)
    } catch {
      toast.error(refundPaymentId ? "Не удалось загрузить платёж для возврата" : "Не удалось загрузить данные")
    } finally {
      setLoading(false)
    }
  }, [activeMerchantId, refundPaymentId])

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

  const riskToken = riskBalance?.items.find((entry) => entry.tokenId === selectedTokenId)
  const riskEnabled = riskToken?.routingEnabled === true
  const selectedRiskBucket = riskToken?.buckets.find((entry) => entry.riskClass === riskClass)
  const explicitRisk = riskEnabled && (riskClass === "high" || riskClass === "unknown")
  const selectedRiskAmount = (explicitRisk ? selectedRiskBucket?.total : selectedRiskBucket?.free) ?? "0"
  const decimals = riskToken?.decimalPlaces ?? tokenBalance?.decimalPlaces ?? 0
  const balanceRaw = riskEnabled ? selectedRiskAmount : (tokenBalance?.rawAmount ?? "0")
  const balanceMax = riskEnabled
    ? rawMinorToMajor(selectedRiskAmount, riskToken?.decimalPlaces ?? 0)
    : tokenBalance?.humanAmount
  // A refund can take no more than the balance allows and what is left to refund.
  const maxAmount = refund && BigInt(refund.refundableAmount) < BigInt(balanceRaw)
    ? trimFraction(rawMinorToMajor(refund.refundableAmount, decimals))
    : balanceMax
  const refundReasonBytes = new TextEncoder().encode(refundReason.trim()).length
  const refundBlocked = !!refundPaymentId && (!refund || BigInt(refund.refundableAmount) === 0n || refundReasonBytes === 0 || refundReasonBytes > 500)

  const handleCopyAddress = async () => {
    if (!selectedAddress) return
    try {
      await navigator.clipboard.writeText(selectedAddress.address)
      toast.success("Адрес скопирован")
    } catch {
      toast.error("Не удалось скопировать адрес. Выделите его и скопируйте вручную.")
    }
  }

  const handleSubmit = async () => {
    if (!activeMerchantId || !selectedAddress || !amount || !riskBalance || (explicitRisk && !riskAccepted) || refundBlocked) return

    setSubmitting(true)
    try {
      const body: PortalCreateWithdrawalRequest = {
        tokenId: selectedTokenId,
        amount,
        addressId: selectedAddress.id,
        ...(riskEnabled ? { riskClass, riskAccepted } : {}),
        ...(refund ? { refundPaymentId: refund.paymentId, refundReason: refundReason.trim() } : {}),
      }

      const resp = await api.portal.createWithdrawalRequest(activeMerchantId, body)
      toast.success(refund ? `Заявка на возврат ${resp.number} создана` : `Заявка ${resp.number} создана`)
      router.push("/portal/withdrawals/requests")
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
          <h1 className="text-2xl font-bold tracking-tight">{refundPaymentId ? "Возврат платежа" : "Запрос вывода"}</h1>
          <p className="text-sm text-muted-foreground">
            {refundPaymentId
              ? "Сумма возврата списывается с вашего баланса и уходит плательщику после одобрения. Комиссия за обработку не возвращается."
              : "Создайте заявку на вывод средств. Заявка будет отправлена на одобрение."}
          </p>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-5">
        {/* Form */}
        <Card className="min-w-0 lg:col-span-3">
          <CardHeader>
            <CardTitle className="text-base">{refundPaymentId ? "Параметры возврата" : "Параметры вывода"}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {refund && (
              <dl className="grid gap-2 rounded-md border bg-muted/40 p-3 text-sm" data-testid="refund-payment">
                <div className="flex flex-wrap justify-between gap-x-3"><dt className="text-muted-foreground">Инвойс</dt><dd className="font-medium">{refund.invoiceNumber || "—"}</dd></div>
                <div className="flex flex-wrap justify-between gap-x-3"><dt className="text-muted-foreground">Платёж</dt><dd className="font-mono tabular-nums">{formatMinorUnits(refund.amount, decimals)} {selectedCurrency?.symbol}</dd></div>
                <div className="flex flex-wrap justify-between gap-x-3"><dt className="text-muted-foreground">Уже в возврате</dt><dd className="font-mono tabular-nums">{formatMinorUnits(refund.committedAmount, decimals)} {selectedCurrency?.symbol}</dd></div>
                <div className="flex flex-wrap justify-between gap-x-3"><dt className="text-muted-foreground">Доступно к возврату</dt><dd className="font-mono font-medium tabular-nums">{formatMinorUnits(refund.refundableAmount, decimals)} {selectedCurrency?.symbol}</dd></div>
                {refund.fromAddress && <div className="flex min-w-0 flex-col gap-1"><dt className="text-muted-foreground">Отправитель платежа</dt><dd className="break-all font-mono text-xs">{refund.fromAddress}</dd><dd className="text-xs text-muted-foreground">Адрес отправителя может принадлежать бирже. Отправляйте возврат только на адрес, который подтвердил плательщик.</dd></div>}
              </dl>
            )}
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
                  <Label htmlFor="withdrawal-token">Токен</Label>
                  <Select value={selectedTokenId} disabled={!!refundPaymentId} onValueChange={(v) => {
                    setSelectedTokenId(v)
                    setSelectedAddressId("") // reset address when token changes
                    setRiskClass("low")
                    setRiskAccepted(false)
                  }}>
                    <SelectTrigger id="withdrawal-token">
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

                {riskEnabled && <FieldGroup>
                  <Field>
                    <FieldLabel>Класс средств</FieldLabel>
                    <ToggleGroup
                      className="flex-wrap justify-start"
                      type="single"
                      variant="outline"
                      value={riskClass}
                      aria-label="Класс риска средств"
                      onValueChange={(value) => {
                        if (isRiskClass(value)) {
                          setRiskClass(value)
                          setRiskAccepted(false)
                        }
                      }}
                    >
                      {(Object.keys(riskLabels) as WithdrawalRiskClass[]).map((value) => (
                        <ToggleGroupItem key={value} value={value}>{riskLabels[value]}</ToggleGroupItem>
                      ))}
                    </ToggleGroup>
                    <FieldDescription>
                      Будут использованы только средства выбранного класса. {explicitRisk ? "Баланс класса" : "Доступно"}: {formatMinorUnits(selectedRiskAmount, riskToken?.decimalPlaces ?? 0)} {selectedCurrency?.symbol}.
                    </FieldDescription>
                  </Field>
                  {explicitRisk && <Field orientation="horizontal">
                    <Checkbox id="risk-accepted" checked={riskAccepted} onCheckedChange={(value) => setRiskAccepted(value === true)} />
                    <FieldLabel htmlFor="risk-accepted">{riskClass === "high" ? "Подтверждаю вывод средств с высоким риском" : "Подтверждаю вывод средств с неизвестным риском"}</FieldLabel>
                  </Field>}
                </FieldGroup>}
                {/* Address (filtered by token's network) */}
                <div className="space-y-2">
                  <Label htmlFor="withdrawal-address">Адрес назначения</Label>
                  <Select
                    value={selectedAddressId}
                    onValueChange={setSelectedAddressId}
                    disabled={!selectedTokenId}
                  >
                    <SelectTrigger id="withdrawal-address">
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

                {refundPaymentId && (
                  <Field>
                    <FieldLabel htmlFor="refund-reason">Причина возврата</FieldLabel>
                    <Textarea id="refund-reason" value={refundReason} maxLength={500} onChange={(e) => setRefundReason(e.target.value)} placeholder="Например: заказ отменён по просьбе покупателя" />
                    <FieldDescription>Сохраняется в заявке и видна при одобрении. До 500 байт.</FieldDescription>
                  </Field>
                )}

                {/* Amount */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <Label htmlFor="withdrawal-amount">Сумма</Label>
                    {tokenBalance && (
                      <button
                        type="button"
                        className="text-[11px] text-primary hover:underline"
                        onClick={() => { if (maxAmount) setAmount(maxAmount) }}
                      >
                        Макс: {maxAmount} {selectedCurrency?.symbol}
                      </button>
                    )}
                  </div>
                  <div className="relative">
                    <Input
                      id="withdrawal-amount"
                      type="text"
                      inputMode="decimal"
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
        <Card className="min-w-0 h-fit lg:col-span-2" role="region" aria-labelledby="withdrawal-summary-title">
          <CardHeader>
            <CardTitle id="withdrawal-summary-title" className="text-base">Сводка</CardTitle>
            <CardDescription>Проверьте сумму, сеть и адрес перед отправкой.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 text-sm">
            <dl className="flex min-w-0 flex-col gap-3">
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <dt className="text-muted-foreground">Токен</dt>
                <dd className="font-medium">
                  {selectedCurrency ? `${selectedCurrency.symbol} (${selectedCurrency.network})` : "—"}
                </dd>
              </div>
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <dt className="text-muted-foreground">Сумма</dt>
                <dd className="break-all font-mono font-medium tabular-nums">
                  {amount || "0.00"} {selectedCurrency?.symbol ?? ""}
                </dd>
              </div>
              <div className="flex min-w-0 flex-col gap-2">
                <dt className="text-muted-foreground">Адрес получателя</dt>
                <dd className="flex min-w-0 flex-col gap-2">
                  <p className="break-all font-mono leading-relaxed select-all" dir="ltr" data-testid="withdrawal-recipient-address">
                    {selectedAddress?.address ?? "Не выбран"}
                  </p>
                  <Button type="button" variant="outline" size="sm" className="self-start" onClick={handleCopyAddress} disabled={!selectedAddress}>
                    <Copy data-icon="inline-start" />
                    Скопировать адрес
                  </Button>
                </dd>
              </div>
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <dt className="text-muted-foreground">Статус</dt>
                <dd>
                  <Badge variant="outline" className="text-amber-500 text-xs">
                    Ожидает одобрения
                  </Badge>
                </dd>
              </div>
            </dl>

            {riskEnabled && <p>Класс средств: <Badge variant="outline">{riskLabels[riskClass]}</Badge></p>}
            <Separator />

            <Button
              className="w-full"
              onClick={handleSubmit}
              disabled={submitting || loading || !riskBalance || !amount || !selectedAddress || (explicitRisk && !riskAccepted) || refundBlocked}
            >
              {submitting ? (
                <Loader2 className="size-4 mr-1.5 animate-spin" />
              ) : (
                <Send className="size-4 mr-1.5" />
              )}
              {refundPaymentId ? "Отправить возврат" : "Отправить заявку"}
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

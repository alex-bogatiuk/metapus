"use client"

import { useEffect, useMemo, useState } from "react"
import Link from "next/link"
import { useRouter } from "next/navigation"
import useSWR from "swr"
import { ArrowLeft, CircleAlert, Loader2, Plus, Receipt } from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel, FieldSeparator } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import { getNetworkColor } from "@/lib/blockchain"
import {
  derivePortalNetworks,
  filterInvoiceTokensByNetwork,
  resolveInvoiceNetworkId,
  resolveInvoiceTokenId,
  walletPoolRetryDelaySeconds,
  type MerchantTokenSelection,
} from "@/lib/portal-invoice-state"
import { cn } from "@/lib/utils"
import { usePortalStore } from "@/stores/usePortalStore"
import type { PortalCreateInvoiceRequest, PortalCurrencyItem } from "@/types/portal-api"

const TTL_OPTIONS = [
  { value: "default", label: "По умолчанию (настройка мерчанта)" },
  { value: "15", label: "15 минут" },
  { value: "30", label: "30 минут" },
  { value: "60", label: "1 час" },
  { value: "120", label: "2 часа" },
  { value: "360", label: "6 часов" },
  { value: "720", label: "12 часов" },
  { value: "1440", label: "24 часа" },
  { value: "10080", label: "Неделя" },
] as const

const EMPTY_TOKENS: PortalCurrencyItem[] = []
interface PoolRetryState extends MerchantTokenSelection {
  retryAtMs: number
}

function fetchPortalCurrencies([, merchantId]: readonly [string, string]) {
  return api.portal.currencies(merchantId)
}

export default function CreateInvoicePage() {
  const router = useRouter()
  const activeMerchantId = usePortalStore((state) => state.activeMerchantId)
  const currenciesKey = activeMerchantId
    ? (["portal-invoice-currencies", activeMerchantId] as const)
    : null
  const {
    data: currencies,
    error: currenciesError,
    isLoading: tokensLoading,
    isValidating: tokensValidating,
    mutate: reloadTokens,
  } = useSWR(currenciesKey, fetchPortalCurrencies)
  const tokens = currencies?.items ?? EMPTY_TOKENS

  const networks = useMemo(() => derivePortalNetworks(tokens), [tokens])
  const [tokenSelection, setTokenSelection] = useState<MerchantTokenSelection | null>(null)

  const selectedNetworkId = resolveInvoiceNetworkId(networks, activeMerchantId, tokenSelection)
  const activeNetwork = networks.find((net) => net.networkId === selectedNetworkId)
  const networkTokens = useMemo(
    () => filterInvoiceTokensByNetwork(tokens, selectedNetworkId),
    [tokens, selectedNetworkId]
  )
  const selectedTokenId = resolveInvoiceTokenId(tokens, activeMerchantId, tokenSelection)
  const activeToken = networkTokens.find((token) => token.tokenId === selectedTokenId)

  const [amount, setAmount] = useState("")
  const [ttl, setTtl] = useState("default")
  const [description, setDescription] = useState("")
  const [orderId, setOrderId] = useState("")
  const [customerId, setCustomerId] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [poolRetry, setPoolRetry] = useState<PoolRetryState | null>(null)
  const [clockMs, setClockMs] = useState(() => Date.now())

  const activePoolRetry =
    poolRetry?.merchantId === activeMerchantId && poolRetry.tokenId === selectedTokenId
      ? poolRetry
      : null
  const poolRetrySeconds = activePoolRetry
    ? Math.max(0, Math.ceil((activePoolRetry.retryAtMs - clockMs) / 1000))
    : 0

  useEffect(() => {
    if (!poolRetry) return

    const timer = window.setInterval(() => {
      const now = Date.now()
      setClockMs(now)
      if (now >= poolRetry.retryAtMs) window.clearInterval(timer)
    }, 1_000)

    return () => window.clearInterval(timer)
  }, [poolRetry])

  const handleSubmit = async () => {
    if (!activeMerchantId || !activeToken || !amount) return

    setPoolRetry(null)
    setSubmitting(true)
    try {
      const body: PortalCreateInvoiceRequest = {
        tokenId: activeToken.tokenId,
        amount,
        ttlMinutes: ttl === "default" ? undefined : Number.parseInt(ttl, 10),
        description: description || undefined,
        orderId: orderId || undefined,
        customerId: customerId.trim() || undefined,
      }

      const response = await api.portal.createInvoice(activeMerchantId, body)
      toast.success(`Инвойс ${response.number} создан`)
      router.push(`/portal/invoices/${response.id}`)
    } catch (error) {
      const retryAfterSeconds = walletPoolRetryDelaySeconds(error)
      if (retryAfterSeconds !== null) {
        const now = Date.now()
        setClockMs(now)
        setPoolRetry({
          merchantId: activeMerchantId,
          networkId: activeToken.networkId,
          tokenId: activeToken.tokenId,
          retryAtMs: now + retryAfterSeconds * 1_000,
        })
      } else {
        toast.error(error instanceof Error ? error.message : "Ошибка создания инвойса")
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (!activeMerchantId) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
        <Receipt className="mb-3 size-10 opacity-40" />
        <p>Выберите мерчанта</p>
      </div>
    )
  }

  return (
    <div className="flex max-w-2xl flex-col gap-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" asChild>
          <Link href="/portal/invoices" aria-label="Вернуться к списку инвойсов">
            <ArrowLeft />
          </Link>
        </Button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Создать инвойс</h1>
          <p className="text-sm text-muted-foreground">
            Создайте платёжный запрос для получения криптовалюты
          </p>
        </div>
      </div>

      <form
        className="grid gap-6 md:grid-cols-5"
        onSubmit={(event) => {
          event.preventDefault()
          void handleSubmit()
        }}
      >
        <Card className="md:col-span-3">
          <CardHeader>
            <CardTitle className="text-base">Параметры инвойса</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {activePoolRetry ? (
              <Alert>
                <CircleAlert />
                <AlertTitle>Платёжный адрес временно недоступен</AlertTitle>
                <AlertDescription>
                  В пуле пока нет свободного адреса. Введённые данные сохранены — повторите создание
                  {poolRetrySeconds > 0 ? ` через ${poolRetrySeconds} с.` : "."}
                </AlertDescription>
              </Alert>
            ) : null}

            {tokensLoading ? (
              <div className="flex flex-col gap-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : currenciesError ? (
              <Alert variant="destructive">
                <CircleAlert />
                <AlertTitle>Не удалось загрузить токены</AlertTitle>
                <AlertDescription className="flex flex-col items-start gap-3">
                  <p>Проверьте подключение и повторите запрос.</p>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={tokensValidating}
                    onClick={() => void reloadTokens()}
                  >
                    Повторить
                  </Button>
                </AlertDescription>
              </Alert>
            ) : tokens.length === 0 ? (
              <Alert>
                <CircleAlert />
                <AlertTitle>Нет доступных токенов</AlertTitle>
                <AlertDescription>
                  Для этого мерчанта пока нельзя создать криптовалютный инвойс.
                </AlertDescription>
              </Alert>
            ) : (
              <FieldGroup>
                <Field>
                  <FieldLabel htmlFor="invoice-network">Сеть</FieldLabel>
                  <Select
                    value={selectedNetworkId}
                    onValueChange={(networkId) =>
                      setTokenSelection({
                        merchantId: activeMerchantId,
                        networkId,
                        tokenId: "",
                      })
                    }
                  >
                    <SelectTrigger id="invoice-network">
                      <SelectValue placeholder="Выберите блокчейн-сеть" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {networks.map((net) => (
                          <SelectItem key={net.networkId} value={net.networkId}>
                            <span className="flex items-center gap-2">
                              <span
                                className={cn(
                                  "size-2 rounded-full shrink-0",
                                  getNetworkColor(net.network)
                                )}
                              />
                              <span>{net.network}</span>
                              <span className="text-xs text-muted-foreground">
                                ({net.symbols.join(", ")})
                              </span>
                            </span>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    Определяет блокчейн, в котором плательщик должен отправить средства.
                  </FieldDescription>
                </Field>

                <Field data-disabled={!selectedNetworkId || undefined}>
                  <FieldLabel htmlFor="invoice-token">Токен</FieldLabel>
                  <Select
                    value={selectedTokenId}
                    disabled={!selectedNetworkId}
                    onValueChange={(tokenId) =>
                      setTokenSelection({
                        merchantId: activeMerchantId,
                        networkId: selectedNetworkId,
                        tokenId,
                      })
                    }
                  >
                    <SelectTrigger id="invoice-token" disabled={!selectedNetworkId}>
                      <SelectValue
                        placeholder={
                          selectedNetworkId ? "Выберите токен" : "Сначала выберите сеть"
                        }
                      />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {networkTokens.map((token) => (
                          <SelectItem key={token.tokenId} value={token.tokenId}>
                            {token.symbol}
                            <span className="ml-1.5 text-xs text-muted-foreground">
                              {token.network}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    {selectedNetworkId
                      ? `Доступны только действующие токены в сети ${activeNetwork?.network ?? ""}.`
                      : "Поле станет доступно после выбора блокчейн-сети."}
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel htmlFor="invoice-amount">Сумма</FieldLabel>
                  <Input
                    id="invoice-amount"
                    type="number"
                    step={
                      activeToken
                        ? (1 / Math.pow(10, activeToken.decimalPlaces)).toFixed(activeToken.decimalPlaces)
                        : "0.01"
                    }
                    min={activeToken ? Math.pow(10, -activeToken.decimalPlaces) : 0.01}
                    placeholder="0.00"
                    value={amount}
                    required
                    onChange={(event) => setAmount(event.target.value)}
                  />
                  {activeToken ? (
                    <FieldDescription>
                      Сумма в {activeToken.symbol}, точность — {activeToken.decimalPlaces} знаков после запятой.
                    </FieldDescription>
                  ) : null}
                </Field>

                <Field>
                  <FieldLabel htmlFor="invoice-ttl">Время жизни</FieldLabel>
                  <Select value={ttl} onValueChange={setTtl}>
                    <SelectTrigger id="invoice-ttl">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {TTL_OPTIONS.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>После истечения инвойс автоматически отменится.</FieldDescription>
                </Field>

                <FieldSeparator />

                <Field>
                  <FieldLabel htmlFor="invoice-description">
                    Описание <span className="text-muted-foreground">(необязательно)</span>
                  </FieldLabel>
                  <Textarea
                    id="invoice-description"
                    placeholder="Оплата заказа #123"
                    value={description}
                    onChange={(event) => setDescription(event.target.value)}
                    rows={2}
                  />
                </Field>

                <Field>
                  <FieldLabel htmlFor="invoice-order-id">
                    Order ID <span className="text-muted-foreground">(необязательно)</span>
                  </FieldLabel>
                  <Input
                    id="invoice-order-id"
                    placeholder="order-123"
                    value={orderId}
                    onChange={(event) => setOrderId(event.target.value)}
                  />
                  <FieldDescription>
                    Ключ идемпотентности — повторный запрос с тем же Order ID вернёт существующий инвойс.
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel htmlFor="invoice-customer-id">
                    Идентификатор клиента <span className="text-muted-foreground">(необязательно)</span>
                  </FieldLabel>
                  <Input
                    id="invoice-customer-id"
                    type="text"
                    placeholder="customer@example.com, user_10492 или любой строковый ID"
                    value={customerId}
                    maxLength={255}
                    onChange={(event) => setCustomerId(event.target.value)}
                  />
                  <FieldDescription>
                    Можно указать email, внутренний ID пользователя или любой другой строковый идентификатор из вашей системы для поиска и сверки платежей.
                  </FieldDescription>
                </Field>
              </FieldGroup>
            )}
          </CardContent>
        </Card>

        <Card className="h-fit md:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">Предпросмотр</CardTitle>
            <CardDescription>Инвойс будет создан с этими параметрами</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 text-sm">
            <div className="flex justify-between gap-3">
              <span className="text-muted-foreground">Сеть</span>
              <span className="font-medium flex items-center gap-1.5">
                {activeNetwork ? (
                  <>
                    <span
                      className={cn(
                        "size-2 rounded-full shrink-0",
                        getNetworkColor(activeNetwork.network)
                      )}
                    />
                    {activeNetwork.network}
                  </>
                ) : (
                  "—"
                )}
              </span>
            </div>
            <div className="flex justify-between gap-3">
              <span className="text-muted-foreground">Токен</span>
              <span className="font-medium">
                {activeToken ? activeToken.symbol : "—"}
              </span>
            </div>
            <div className="flex justify-between gap-3">
              <span className="text-muted-foreground">Сумма</span>
              <span className="font-mono font-medium tabular-nums">
                {amount || "0.00"} {activeToken?.symbol ?? ""}
              </span>
            </div>
            <div className="flex justify-between gap-3">
              <span className="text-muted-foreground">TTL</span>
              <span>{TTL_OPTIONS.find((option) => option.value === ttl)?.label ?? ttl}</span>
            </div>
            {description ? (
              <div className="flex justify-between gap-3">
                <span className="text-muted-foreground">Описание</span>
                <span className="max-w-[160px] truncate text-right">{description}</span>
              </div>
            ) : null}
            {orderId ? (
              <div className="flex justify-between gap-3">
                <span className="text-muted-foreground">Order ID</span>
                <span className="font-mono text-xs">{orderId}</span>
              </div>
            ) : null}
            {customerId.trim() ? (
              <div className="flex justify-between gap-3">
                <span className="text-muted-foreground">Клиент</span>
                <span className="max-w-[160px] truncate font-mono text-xs text-right">
                  {customerId.trim()}
                </span>
              </div>
            ) : null}

            <Separator />

            <Button
              className="w-full"
              type="submit"
              disabled={submitting || !amount || !activeToken || poolRetrySeconds > 0}
            >
              {submitting ? (
                <Loader2 data-icon="inline-start" className="animate-spin" />
              ) : (
                <Plus data-icon="inline-start" />
              )}
              {submitting
                ? "Создание…"
                : poolRetrySeconds > 0
                  ? `Повторить через ${poolRetrySeconds} с`
                  : "Создать инвойс"}
            </Button>
          </CardContent>
        </Card>
      </form>
    </div>
  )
}

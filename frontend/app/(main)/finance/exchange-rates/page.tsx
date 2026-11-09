"use client"
import { useRouter, useSearchParams } from "next/navigation"
import useSWR from "swr"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle
} from "@/components/ui/sheet"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { QuoteFacts } from "@/components/exchange-rates/quote-facts"
import {
  exchangeRatesApi,
  formatDecimal,
  rateReasonLabels,
  rateStatusLabels,
  rateTime
} from "@/lib/exchange-rates"
import { usePermissions } from "@/hooks/use-permissions"

export default function ExchangeRatesPage() {
  const router = useRouter()
  const search = useSearchParams()
  const { has, loading: permissionsLoading, error: permissionsError } = usePermissions()
  const allowed = has("register:exchange_rate:read")
  const setParams = (changes: Record<string, string | null>) => {
    const next = new URLSearchParams(search.toString())
    for (const [key, value] of Object.entries(changes)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    router.replace(`/finance/exchange-rates?${next}`, { scroll: false })
  }
  const params = new URLSearchParams()
  for (const key of ["currencyId", "status", "cursor"]) {
    const value = search.get(key)
    if (value) params.set(key, value)
  }
  const current = useSWR(allowed ? ["exchange-rates", params.toString()] : null, () =>
    exchangeRatesApi.current(params)
  )
  const currencies = useSWR(allowed ? "exchange-rate-currencies" : null, () =>
    exchangeRatesApi.currencies()
  )
  const historyCurrency = search.get("history")
  const historyParams = new URLSearchParams({ currencyId: historyCurrency ?? "" })
  for (const [urlKey, apiKey] of [
    ["from", "from"],
    ["to", "to"],
    ["historyCursor", "cursor"]
  ]) {
    const value = search.get(urlKey)
    if (value) historyParams.set(apiKey, value)
  }
  const history = useSWR(
    allowed && historyCurrency ? ["exchange-history", historyParams.toString()] : null,
    () => exchangeRatesApi.history(historyParams)
  )
  const quoteID = search.get("quote")
  const quote = useSWR(allowed && quoteID ? ["exchange-quote", quoteID] : null, () =>
    exchangeRatesApi.quote(quoteID!)
  )
  const row = current.data?.items.find((item) => item.currency.id === search.get("details"))
  const selected = quote.data ?? row?.selectedQuote ?? row?.lastKnownQuote
  const detailsOpen = !!quoteID || !!search.get("details")
  if (permissionsLoading)
    return (
      <p role="status" className="p-6">
        Проверка прав доступа…
      </p>
    )
  if (permissionsError)
    return (
      <p role="alert" className="p-6">
        Не удалось проверить права доступа. Обновите страницу.
      </p>
    )
  if (!allowed)
    return (
      <p role="alert" className="p-6">
        Нет права на просмотр курсов валют
      </p>
    )
  return (
    <div className="min-h-0 flex-1 space-y-5 overflow-y-auto p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold">Курсы валют</h1>
        <Button
          variant="outline"
          disabled={current.isValidating}
          onClick={() => void current.mutate()}
        >
          Обновить список
        </Button>
      </div>
      {current.data && (
        <p className="text-sm text-muted-foreground">
          Валюта оценки: {current.data.baseCurrency.code} ·{" "}
          {current.data.source?.name ?? "Источник не выбран"} · Загрузка каждые{" "}
          {current.data.intervalSeconds / 60} мин. · Актуальность {current.data.maxAgeSeconds / 60}{" "}
          мин. · Оценка на {rateTime(current.data.evaluatedAt)}
        </p>
      )}
      {current.data?.feedHealth &&
        ["failed", "partial"].includes(current.data.feedHealth.status) && (
          <p
            role="status"
            className="rounded-md border border-amber-300 p-3 text-sm text-amber-800 dark:text-amber-300"
          >
            {current.data.feedHealth.status === "partial"
              ? "При последней загрузке получена часть курсов."
              : "Последняя загрузка курсов завершилась с ошибкой."}{" "}
            Попытка: {rateTime(current.data.feedHealth.lastAttemptAt)}. Актуальность каждого
            сохранённого курса указана в списке.
          </p>
        )}
      <div className="flex flex-wrap gap-4">
        <div className="w-64 space-y-2">
          <Label htmlFor="rate-currency">Валюта</Label>
          <Select
            value={search.get("currencyId") ?? "all"}
            onValueChange={(value) =>
              setParams({ currencyId: value === "all" ? null : value, cursor: null })
            }
          >
            <SelectTrigger id="rate-currency">
              <SelectValue placeholder="Все валюты" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Все валюты</SelectItem>
              {currencies.data?.map((currency) => (
                <SelectItem key={currency.id} value={currency.id}>
                  {currency.code} · {currency.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="w-56 space-y-2">
          <Label htmlFor="rate-status">Состояние</Label>
          <Select
            value={search.get("status") ?? "all"}
            onValueChange={(value) =>
              setParams({ status: value === "all" ? null : value, cursor: null })
            }
          >
            <SelectTrigger id="rate-status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">Все состояния</SelectItem>
              {Object.entries(rateStatusLabels).map(([value, label]) => (
                <SelectItem key={value} value={value}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      {current.error && <p role="alert">Не удалось загрузить курсы: {current.error.message}</p>}
      {current.isLoading && <p role="status">Загрузка курсов…</p>}
      <Table>
        <TableHeader>
          <TableRow>
            {[
              "Валюта",
              "Кратность",
              "Курс к USD",
              "Источник",
              "Курс на",
              "Получен",
              "Состояние",
              ""
            ].map((label, index) => (
              <TableHead key={index}>{label}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {current.data?.items.map((item) => {
            const known = item.selectedQuote ?? item.lastKnownQuote
            return (
              <TableRow key={item.currency.id}>
                <TableCell>
                  <Button
                    variant="link"
                    onClick={() => setParams({ details: item.currency.id, quote: null })}
                  >
                    {item.currency.code}
                  </Button>
                </TableCell>
                <TableCell>{item.status === "identity" ? 1 : (known?.multiplier ?? "—")}</TableCell>
                <TableCell className="tabular-nums">
                  {item.status === "identity" ? "1" : formatDecimal(known?.rate ?? null)}
                </TableCell>
                <TableCell>{item.status === "identity" ? "—" : known?.source.name ?? current.data?.source?.name ?? "—"}</TableCell>
                <TableCell>{rateTime(known?.providerUpdatedAt ?? null)}</TableCell>
                <TableCell>{rateTime(known?.receivedAt ?? null)}</TableCell>
                <TableCell>
                  <Badge
                    variant={
                      item.status === "fresh" || item.status === "identity"
                        ? "secondary"
                        : "outline"
                    }
                  >
                    {rateStatusLabels[item.status]}
                  </Badge>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {rateReasonLabels[item.reason]}
                  </p>
                </TableCell>
                <TableCell>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setParams({ history: item.currency.id, historyCursor: null })}
                  >
                    История
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
          {current.data?.items.length === 0 && (
            <TableRow>
              <TableCell colSpan={8}>По выбранным фильтрам курсов нет</TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <div className="flex gap-2">
        {search.has("cursor") && (
          <Button variant="outline" onClick={() => setParams({ cursor: null })}>
            В начало
          </Button>
        )}
        {current.data?.nextCursor && (
          <Button variant="outline" onClick={() => setParams({ cursor: current.data!.nextCursor })}>
            Следующие 50
          </Button>
        )}
      </div>
      <Sheet
        open={!!historyCurrency && !detailsOpen}
        onOpenChange={(open) => {
          if (!open) setParams({ history: null, historyCursor: null })
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-3xl">
          <SheetHeader>
            <SheetTitle>История курса</SheetTitle>
            <SheetDescription>
              Все сохранённые наблюдения. Период запроса — до 31 дня.
            </SheetDescription>
          </SheetHeader>
          <div className="my-5 grid grid-cols-2 gap-3">
            {["from", "to"].map((key) => (
              <div key={key} className="space-y-2">
                <Label htmlFor={`history-${key}`}>{key === "from" ? "С" : "До (не включая)"}</Label>
                <Input
                  id={`history-${key}`}
                  type="date"
                  value={
                    search.get(key)?.slice(0, 10) ??
                    history.data?.[key as "from" | "to"].slice(0, 10) ??
                    ""
                  }
                  onChange={(event) => {
                    if (event.target.value)
                      setParams({ [key]: `${event.target.value}T00:00:00Z`, historyCursor: null })
                  }}
                />
              </div>
            ))}
          </div>
          {history.error && <p role="alert">{history.error.message}</p>}
          {history.isLoading && <p role="status">Загрузка истории…</p>}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Курс на</TableHead>
                <TableHead>Курс</TableHead>
                <TableHead>Версия</TableHead>
                <TableHead>Источник</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.data?.items.map((item) => (
                <TableRow key={item.id}>
                  <TableCell>
                    <Button variant="link" onClick={() => setParams({ quote: item.id })}>
                      {rateTime(item.providerUpdatedAt)}
                    </Button>
                  </TableCell>
                  <TableCell>{formatDecimal(item.rate)}</TableCell>
                  <TableCell>{item.version}</TableCell>
                  <TableCell>{item.source.name}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {history.data?.items.length === 0 && (
            <p className="py-4 text-sm text-muted-foreground">За этот период наблюдений нет</p>
          )}
          <div className="mt-4 flex gap-2">
            {search.has("historyCursor") && (
              <Button variant="outline" onClick={() => setParams({ historyCursor: null })}>
                В начало
              </Button>
            )}
            {history.data?.nextCursor && (
              <Button
                variant="outline"
                onClick={() => setParams({ historyCursor: history.data!.nextCursor })}
              >
                Следующие 50
              </Button>
            )}
          </div>
        </SheetContent>
      </Sheet>
      <Sheet
        open={detailsOpen}
        onOpenChange={(open) => {
          if (!open) setParams({ quote: null, details: null })
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>Котировка</SheetTitle>
            <SheetDescription>
              {row ? rateReasonLabels[row.reason] : "Сохранённое наблюдение"}
            </SheetDescription>
          </SheetHeader>
          <div className="space-y-4 py-6">
            {quote.error && <p role="alert">Не удалось загрузить котировку</p>}
            {quote.isLoading && <p role="status">Загрузка…</p>}
            {row?.status === "identity" && (
              <p>
                1 {row.currency.code} = 1 {row.baseCurrency.code}
              </p>
            )}
            {selected && (
              <>
                <QuoteFacts quote={selected} />
                <p className="text-xs text-muted-foreground">
                  Версия соответствия: {selected.mappingVersion}
                </p>
                <p className="break-all text-xs text-muted-foreground">
                  Попытка импорта: {selected.feedAttemptId}
                </p>
              </>
            )}
            {row && !row.selectedQuote && row.lastKnownQuote && (
              <p className="text-sm">Последний известный курс; для оценки не применяется.</p>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}

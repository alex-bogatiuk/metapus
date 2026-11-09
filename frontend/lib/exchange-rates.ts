import Decimal from "decimal.js"
import { apiFetch } from "@/lib/api"
import { z } from "zod"
import * as Wire from "@/lib/generated/http-contract-schemas"
import type {
  CurrentRates,
  RateCurrency,
  RateFeedState,
  RateHistory,
  RateMapping,
  RegisterQuote
} from "@/types/exchange-rates"

const rateStatusSchema = z.enum(["fresh", "stale", "missing", "disabled", "identity"])
const currentRatesSchema = Wire.CurrentExchangeRatesResponseSchema.transform(page => ({
  ...page,
  items: page.items.map(item => ({ ...item,
    currency: Wire.ExchangerateCurrencyRefSchema.parse(item.currency),
    status: rateStatusSchema.parse(item.status),
  })),
})) satisfies z.ZodType<CurrentRates>

const currentPage = (params: URLSearchParams) => apiFetch(`/registers/exchange-rates/current?${params}`)
  .then(value => currentRatesSchema.parse(value))

export function formatDecimal(value: string | null, places?: number): string {
  if (value === null) return "—"
  const decimal = new Decimal(value)
  const text =
    places === undefined ? decimal.toFixed() : decimal.toFixed(places, Decimal.ROUND_HALF_UP)
  const [integer, fraction] = text.split(".")
  return integer.replace(/\B(?=(\d{3})+(?!\d))/g, "\u00a0") + (fraction ? `,${fraction}` : "")
}
export const rateStatusLabels = {
  fresh: "Актуален",
  stale: "Устарел",
  missing: "Нет курса",
  disabled: "Выключен",
  identity: "Без пересчёта"
}
export const rateReasonLabels: Record<string, string> = {
  no_quote: "Курс ещё не получен",
  source_not_configured: "Источник не выбран",
  source_disabled: "Источник выключен",
  mapping_missing: "Соответствие не настроено",
  mapping_disabled: "Соответствие выключено",
  mapping_changed: "Ожидается курс по новому соответствию",
  quote_not_effective: "Время котировки ещё не наступило",
  quote_stale: "Срок актуальности истёк",
  selected: "Курс актуален",
  same_currency: "Валюта совпадает с валютой оценки",
  currency_not_linked: "У токена не указана валюта",
  currency_missing: "Валюта недоступна",
  currency_unavailable: "Валюта недоступна"
}
export function rateTime(value: string | null): string {
  return value ? new Date(value).toLocaleString("ru-RU") : "—"
}
export const exchangeRatesApi = {
  currencies: async () => {
    const items: RateCurrency[] = []
    let cursor: string | null = null
    do {
      const params = new URLSearchParams({ limit: "100" })
      if (cursor) params.set("cursor", cursor)
      const page = await currentPage(params)
      items.push(...page.items.map((item) => item.currency))
      cursor = page.nextCursor
    } while (cursor)
    return items
  },
  current: currentPage,
  history: (params: URLSearchParams) =>
    apiFetch<RateHistory>(`/registers/exchange-rates/history?${params}`),
  quote: (id: string) =>
    apiFetch<RegisterQuote>(`/registers/exchange-rates/${encodeURIComponent(id)}`),
  mappings: (source: string) =>
    apiFetch<{ items: RateMapping[]; baseCurrency: RateCurrency }>(
      `/catalog/rate-sources/${source}/mappings`
    ),
  feedState: (source: string) =>
    apiFetch<RateFeedState>(`/catalog/rate-sources/${source}/feed-state`),
  putMapping: (
    source: string,
    currency: string,
    base: string,
    body: { externalId: string; isActive: boolean; expectedVersion: number }
  ) =>
    apiFetch<RateMapping>(`/catalog/rate-sources/${source}/mappings/${currency}/${base}`, {
      method: "PUT",
      body: JSON.stringify(body)
    })
}

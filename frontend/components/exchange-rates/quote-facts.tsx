import { formatDecimal, rateTime } from "@/lib/exchange-rates"
import type { RateQuote } from "@/types/exchange-rates"

export function QuoteFacts({ quote }: { quote: RateQuote }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
      <dt>Курс</dt>
      <dd className="break-words tabular-nums">
        {quote.multiplier} {quote.currency.code} = {formatDecimal(quote.rate)}{" "}
        {quote.baseCurrency.code}
      </dd>
      <dt>Источник</dt>
      <dd>{quote.source.name}</dd>
      <dt>Курс на</dt>
      <dd>{rateTime(quote.providerUpdatedAt)}</dd>
      <dt>Получен</dt>
      <dd>{rateTime(quote.receivedAt)}</dd>
      <dt>Записан</dt>
      <dd>{rateTime(quote.recordedAt)}</dd>
      <dt>Версия</dt>
      <dd>{quote.version}</dd>
    </dl>
  )
}

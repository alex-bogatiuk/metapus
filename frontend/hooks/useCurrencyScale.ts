"use client"

import { useResolvedRef } from "@/lib/ref-resolver"

export interface CurrencyScale {
  /** Null until the current principal can read the currency's unit. Zero is a valid scale. */
  decimalPlaces: number | null
  symbol: string
  loading: boolean
  error: Error | null
  retry: () => void
}

export function useCurrencyScale(currencyId: string | undefined): CurrencyScale {
  const { resolved, loading, error, retry } = useResolvedRef(currencyId ? { refType: "Currency", refId: currencyId } : null)
  const hasUnit = resolved?.decimalPlaces !== undefined && !!resolved.currencyCode
  return {
    decimalPlaces: hasUnit ? resolved.decimalPlaces ?? null : null,
    symbol: hasUnit ? resolved.currencyCode ?? "" : "",
    loading, error, retry,
  }
}

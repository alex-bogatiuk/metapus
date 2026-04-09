import { useState, useEffect, useMemo } from "react"
import { apiFetch } from "@/lib/api"
import { DEFAULT_DECIMAL_PLACES } from "@/lib/format"

/**
 * Currency scale info returned by the hook.
 * Provides everything needed to format monetary values for a given currency.
 */
export interface CurrencyScale {
  decimalPlaces: number
  symbol: string
  loading: boolean
}

interface CurrencyInfo {
  decimalPlaces: number
  symbol: string
}

// Simple in-memory cache: currencyId → { decimalPlaces, symbol }
const currencyCache = new Map<string, CurrencyInfo>()

/**
 * useCurrencyScale — reusable hook for any CurrencyAware document form.
 *
 * Fetches and caches currency `decimalPlaces` and `symbol` for the given currencyId.
 * When currencyId changes (e.g. user selects a different currency), the hook
 * automatically refetches. Results are cached so repeated selections are instant.
 *
 * Usage in any document form:
 *   const { decimalPlaces, symbol } = useCurrencyScale(f.currencyId)
 *   // Then pass decimalPlaces to toMinorUnits/fromMinorUnits/fmtAmount/moneyStep
 */
export function useCurrencyScale(currencyId: string | undefined): CurrencyScale {
  // Track fetched results via state; synchronous cache hits handled via useMemo
  const [fetchedInfo, setFetchedInfo] = useState<Record<string, CurrencyInfo>>({})
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!currencyId || currencyCache.has(currencyId)) return

    let cancelled = false
    setLoading(true)
    apiFetch<{ decimalPlaces: number; symbol?: string | null }>(`/catalog/currencies/${currencyId}`)
      .then((c) => {
        const info: CurrencyInfo = {
          decimalPlaces: c.decimalPlaces ?? DEFAULT_DECIMAL_PLACES,
          symbol: c.symbol ?? "",
        }
        currencyCache.set(currencyId, info)
        if (!cancelled) setFetchedInfo((prev) => ({ ...prev, [currencyId]: info }))
      })
      .catch(() => { /* fallback handled in useMemo */ })
      .finally(() => { if (!cancelled) setLoading(false) })

    return () => { cancelled = true }
  }, [currencyId])

  return useMemo(() => {
    if (!currencyId) return { decimalPlaces: DEFAULT_DECIMAL_PLACES, symbol: "", loading: false }
    const cached = currencyCache.get(currencyId)
    if (cached) return { decimalPlaces: cached.decimalPlaces, symbol: cached.symbol, loading: false }
    return { decimalPlaces: DEFAULT_DECIMAL_PLACES, symbol: "", loading }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currencyId, loading, fetchedInfo])
}

/**
 * Apply currency scale from a CurrencyRefDisplay (already resolved by backend).
 * Use this when the response already contains currency.decimalPlaces (e.g. edit page).
 */
export function applyCurrencyRef(ref: { decimalPlaces: number; symbol?: string } | undefined | null): CurrencyScale {
  if (!ref) return { decimalPlaces: DEFAULT_DECIMAL_PLACES, symbol: "", loading: false }
  return { decimalPlaces: ref.decimalPlaces, symbol: ref.symbol ?? "", loading: false }
}

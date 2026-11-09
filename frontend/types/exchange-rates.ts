import type * as Wire from "@/types/generated/http-contracts"

export type RateCurrency = Wire.ExchangerateCurrencyRef
export type RateSource = Wire.ExchangerateSourceRef
export type RateStatus = "fresh" | "stale" | "missing" | "disabled" | "identity"
export type RegisterQuote = Wire.ExchangeRateQuoteResponse
export type RateQuote = Omit<RegisterQuote, "mappingVersion" | "feedAttemptId">
// Register pages require an identified currency, validated at the API boundary.
export type RateResolution = Omit<Wire.ExchangerateResolution, "currency" | "status"> & {
  currency: RateCurrency
  status: RateStatus
}
export type CurrentRates = Omit<Wire.CurrentExchangeRatesResponse, "items"> & { items: RateResolution[] }
export type RateHistory = Wire.ExchangeRateHistoryResponse
export type RateMapping = Wire.ExchangeRateMappingResponse
export type RateFeedState = Wire.ExchangeRateFeedStateResponse

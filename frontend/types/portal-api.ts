/**
 * Portal API types — mirrors backend dto/portal_api.go.
 */

export interface PortalSummaryResponse {
  totalInvoices: number
  paidInvoices: number
  pendingInvoices: number
  totalMinorUnits: string
  change24hPct: string
}

export interface PortalCurrencyItem {
  symbol: string
  network: string
  count: number
  totalMinor: string
  sharePct: string
  decimalPlaces: number
}

export interface PortalChartPoint {
  day: string
  deposits: string
}

export interface PortalMerchantItem {
  id: string
  name: string
  code: string
}

export interface PortalInvoiceItem {
  id: string
  number: string
  status: string
  amount: string
  receivedAmount: string
  symbol: string
  network: string
  decimalPlaces: number
  createdAt: string
}

export interface PortalInvoiceListResponse {
  items: PortalInvoiceItem[]
  total: number
}

// ── Conversion Funnel ──────────────────────────────────────────────────────

export interface PortalFunnelResponse {
  total: number
  receivedAny: number
  fullyPaid: number
  confirmed: number
  expired: number
}

// ── Payment Links ──────────────────────────────────────────────────────────

export interface CreatePaymentLinkRequest {
  tokenId: string
  amount: string
  description?: string
  reusable?: boolean
  maxUses?: number
  ttlMinutes?: number
}

export interface PortalPaymentLinkItem {
  id: string
  shortCode: string
  amount: string
  symbol: string
  network: string
  description: string
  reusable: boolean
  maxUses: number
  currentUses: number
  status: string
  ttlMinutes: number
  payUrl: string
  createdAt: string
}

export interface PortalPaymentLinkListResponse {
  items: PortalPaymentLinkItem[]
  total: number
}

export interface PortalPaymentLinkCreateResponse {
  id: string
  shortCode: string
  payUrl: string
}

// ── Merchant Settings ──────────────────────────────────────────────────────

export interface PortalSettingsResponse {
  webhookUrl: string
  defaultTtlMinutes: number
}

export interface UpdatePortalSettingsRequest {
  webhookUrl?: string | null
  defaultTtlMinutes?: number | null
}

// ── Balance (Fiat Valuation) ───────────────────────────────────────────────

export interface PortalTokenBalance {
  tokenId: string
  tokenSymbol: string
  currencyCode: string
  rawAmount: string
  humanAmount: string
  rate: string
  multiplier: number
  baseAmount: string
  hasRate: boolean
}

export interface PortalBalanceResponse {
  totalBase: string
  baseCurrency: string
  rateSource: string
  byToken: PortalTokenBalance[]
}

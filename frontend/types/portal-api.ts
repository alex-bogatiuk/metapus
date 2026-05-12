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

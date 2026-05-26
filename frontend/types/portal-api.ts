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
  tokenId: string
  symbol: string
  networkId: string
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

  // Payment details (from joined CryptoPayment)
  txHash?: string
  fromAddress?: string
  processingFee?: string
  netAmount?: string
  confirmedAt?: string

  // Invoice metadata
  externalId?: string
  customerEmail?: string
}

export interface PortalInvoiceListResponse {
  items: PortalInvoiceItem[]
  total: number
}

export interface InvoiceFilterParams {
  merchantId?: string
  status?: string
  search?: string
  token?: string
  dateFrom?: string
  dateTo?: string
  sort?: string
  order?: "asc" | "desc"
  limit?: number
  offset?: number
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

// ── Invoice Detail ─────────────────────────────────────────────────────

export interface PortalInvoiceDetailResponse extends PortalInvoiceItem {
  walletAddress: string
  expiresAt: string
  description?: string
  orderId?: string
  timeline: PortalTimelineEvent[]
  webhookDeliveries: PortalWebhookDeliveryItem[]
}

export interface PortalTimelineEvent {
  id: string
  eventType: string
  fromStatus: string
  toStatus: string
  metadata: {
    confirmations?: number
    requiredConfs?: number
    blockNumber?: number
    txHash?: string
  }
  createdAt: string
}

export interface PortalWebhookDeliveryItem {
  id: string
  eventType: string
  deliveryId: string
  statusCode: number | null
  responseTimeMs: number | null
  attempt: number
  errorMessage: string | null
  createdAt: string
}

// ── Withdrawals ────────────────────────────────────────────────────────

export interface PortalWithdrawalItem {
  id: string
  number: string
  status: string
  amount: string
  symbol: string
  network: string
  decimalPlaces: number
  destAddress: string
  txHash?: string
  networkFee?: string
  createdAt: string
  confirmedAt?: string
}

export interface PortalWithdrawalListResponse {
  items: PortalWithdrawalItem[]
  total: number
}

// ── Test Webhook ───────────────────────────────────────────────────────

export interface PortalTestWebhookResponse {
  success: boolean
  statusCode: number | null
  responseTimeMs: number | null
  error: string | null
}

// ── Webhook Delivery List ──────────────────────────────────────────────

export interface PortalWebhookDeliveryListResponse {
  items: PortalWebhookDeliveryItem[]
  total: number
}

// ── Webhook Secret ────────────────────────────────────────────────────

export interface PortalWebhookSecretResponse {
  secret: string
}

// ── Fee Schedule ──────────────────────────────────────────────────────

export interface PortalFeeItem {
  tokenSymbol: string
  network: string
  direction: string // "processing" | "withdrawal"
  fixedFee: string  // minor units as string
  percentBp: number // basis points [0..10000]
  minFee: string    // "0" if no floor
  maxFee: string    // "0" if no cap
  decimalPlaces: number
  isCustom: boolean
}

export interface PortalFeeScheduleResponse {
  items: PortalFeeItem[]
}

// ── Portal Create Invoice ─────────────────────────────────────────────

export interface PortalCreateInvoiceRequest {
  tokenId: string
  amount: string        // human-readable
  ttlMinutes?: number
  description?: string
  orderId?: string
  customerEmail?: string
}

export interface PortalCreateInvoiceResponse {
  id: string
  number: string
  status: string
  amount: string        // minor units
  symbol: string
  network: string
  decimalPlaces: number
  walletAddress: string
  expiresAt: string
  createdAt: string
}

// ── Detailed Balance (three-bucket) ───────────────────────────────────

export interface PortalTokenDetailed extends PortalTokenBalance {
  pendingRaw: string
  pendingHuman: string
  availableRaw: string
  availableHuman: string
}

export interface PortalDetailedBalanceResponse {
  totalBase: string
  pendingBase: string
  availableBase: string
  baseCurrency: string
  rateSource: string
  byToken: PortalTokenDetailed[]
}

// ── Withdrawal Address Whitelist ──────────────────────────────────────

export interface PortalWithdrawalAddress {
  id: string
  networkId: string
  network: string
  address: string
  label: string
  createdAt: string
}

export interface PortalAddWithdrawalAddressRequest {
  networkId: string
  address: string
  label?: string
}

export interface PortalWithdrawalAddressListResponse {
  items: PortalWithdrawalAddress[]
}

// ── Withdrawal Requests ───────────────────────────────────────────────

export interface PortalCreateWithdrawalRequest {
  tokenId: string
  amount: string        // human-readable
  addressId: string     // from whitelist
}

export interface PortalWithdrawalRequestItem {
  id: string
  number: string
  status: string        // pending_approval, approved, signing, broadcast, confirmed, rejected, failed
  amount: string        // minor units
  symbol: string
  network: string
  decimalPlaces: number
  destAddress: string
  rejectionReason?: string
  createdAt: string
  approvedAt?: string
}

export interface PortalWithdrawalRequestListResponse {
  items: PortalWithdrawalRequestItem[]
  total: number
}

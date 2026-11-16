import type { JsonObject } from "@/lib/json-value"
import type { AmlWalletAssetDisplay } from "@/types/generated/http-contracts"
/**
 * Portal API types — mirrors backend dto/portal_api.go.
 */

export type PortalSummaryResponse = import("@/types/generated/http-contracts").PortalSummaryResponse

export interface PortalCurrencyItem {
  tokenId: string
  symbol: string
  networkId: string
  network: string
  count: number
  totalMinor: string
  invoiceSharePct: string
  decimalPlaces: number
}

export interface PortalChartPoint {
  day: string
  deposits: string
  tokenId: string
  symbol: string
  network: string
  decimalPlaces: number
}

export interface PortalMerchantItem {
  id: string
  name: string
  code: string
  role: number
  networkAccess: "allow" | "warn" | "deny"
}

export type MerchantNetworkPolicyMode = "off" | "warn" | "enforce"
export type MerchantNetworkSurface = "portal" | "api"

export interface MerchantAllowedNetwork {
  id: string
  network: string
  label: string
  isActive: boolean
  createdAt: string
}

export interface MerchantNetworkPolicy {
  id: string
  merchantId: string
  surface: MerchantNetworkSurface
  apiKeyId?: string
  mode: MerchantNetworkPolicyMode
  version: number
  allowedNetworks: MerchantAllowedNetwork[]
  updatedAt: string
}

export interface MerchantNetworkPolicyResponse {
  policy: MerchantNetworkPolicy
  detectedIp: string
  networkAccess: "allow" | "warn" | "deny"
}

export interface PortalInvoiceItem {
  id: string
  number: string
  status: string
  amount: string
  receivedAmount: string
  symbol: string
  network: string
  explorerUrl?: string
  decimalPlaces: number
  createdAt: string

  // Totals across current canonical payments, in token minor units.
  paymentCount: number
  processingFee: string
  netAmount: string

  // Invoice metadata
  externalId?: string
  orderId?: string
  customerId?: string
  invoiceKind?: "payment" | "top_up"
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
  publicRef: string
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
  publicRef: string
  payUrl: string
}

// ── Merchant Settings ──────────────────────────────────────────────────────

export interface PortalSettingsResponse {
  defaultTtlMinutes: number
  singlePaymentPerInvoice: boolean
}

export interface UpdatePortalSettingsRequest {
  defaultTtlMinutes?: number | null
  singlePaymentPerInvoice?: boolean | null
}

// ── Balance (Fiat Valuation) ───────────────────────────────────────────────

export interface PortalTokenBalance {
  tokenId: string; tokenSymbol: string; decimalPlaces: number; currencyCode: string
  networkId?: string; network?: string
  rawAmount: string; humanAmount: string; pendingRaw: string; pendingHuman: string
  availableRaw: string; availableHuman: string
  valuation: {
    status: import("../../../types/exchange-rates").RateStatus; reason: string
    selectedQuote: import("../../../types/exchange-rates").RateQuote | null
    lastKnownQuote: import("../../../types/exchange-rates").RateQuote | null
    freshUntil: string | null; total: string | null; pending: string | null; available: string | null
  }
}
export interface ValuationBucket { amount: string | null; coverage: "complete" | "partial" | "none"; unvaluedCount: number }
export interface PortalBalanceResponse {
  baseCurrency: import("../../../types/exchange-rates").RateCurrency
  source: import("../../../types/exchange-rates").RateSource | null
  evaluatedAt: string; freshUntil: string | null
  valuation: { total: ValuationBucket; pending: ValuationBucket; available: ValuationBucket }
  byToken: PortalTokenBalance[]
}

// ── Invoice Detail ─────────────────────────────────────────────────────

export type PortalInvoiceDetailResponse = import("../../../types/generated/http-contracts").PortalInvoiceDetailResponse
export type PortalInvoicePayment = import("../../../types/generated/http-contracts").PortalInvoicePayment
export type PortalTimelineEvent = import("../../../types/generated/http-contracts").PortalTimelineEvent
export type PortalInvoiceLedger = PortalInvoiceDetailResponse["financialSummary"]["ledger"]

export interface PortalWebhookDeliveryItem {
  id: string
  eventType: string
  deliveryId: string
  statusCode: number | null
  responseTimeMs: number | null
  attempt: number
  errorMessage: string | null
  errorClass?: string
  responseBody?: string
  nextRetryAt?: string
  webhookUrl?: string
  createdAt: string
}

// ── Withdrawals ────────────────────────────────────────────────────────

export interface PortalWithdrawalItem {
	 riskClass?: string
  id: string
  number: string
  status: string
  amount: string
  symbol: string
  network: string
  explorerUrl?: string
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

// ── Webhook Delivery List ──────────────────────────────────────────────

export interface PortalWebhookEndpoint {
  id: string
  object: "webhookEndpoint"
  name: string
  url: string
  enabledEvents: string[]
  isDefault: boolean
  isActive: boolean
  secretVersion: number
  version: number
  previousSecretValidUntil?: string
  createdAt: string
  updatedAt: string
  secret?: string
}

export interface PortalMerchantEvent {
  id: string
  object: "event"
  type: string
  apiVersion: string
  resourceVersion: number
  invoiceId?: string
  createdAt: string
  payload: JsonObject
}

export type PortalWebhookTestResponse = import("../../../types/generated/http-contracts").WebhookTestResponse

// ── Webhook Secret ────────────────────────────────────────────────────

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
  customerId?: string
}

export interface PortalCreateInvoiceResponse {
  id: string
  checkoutRef: string
  payUrl: string
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

export type PortalTokenDetailed = PortalTokenBalance
export type PortalDetailedBalanceResponse = PortalBalanceResponse

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

export type WithdrawalRiskClass = "low" | "medium" | "high" | "unknown"

export interface PortalCreateWithdrawalRequest {
 riskClass?: WithdrawalRiskClass
 riskAccepted?: boolean
  tokenId: string
  amount: string        // human-readable
  addressId: string     // from whitelist
  refundPaymentId?: string // makes the request a refund of that credited payment
  refundReason?: string    // required with refundPaymentId
}

export type PortalRefundablePaymentResponse = import("../../../types/generated/http-contracts").PortalRefundablePaymentResponse

export interface PortalWithdrawalRequestItem {
 riskClass?: string
 riskAccepted: boolean
 sources?: import("@/types/generated/http-contracts").PortalWithdrawalSource[]
  id: string
  number: string
  status: string        // pending_signature, pending_approval, approved, signing, broadcast, confirmed, rejected, failed
  amount: string        // minor units
  symbol: string
  network: string
  decimalPlaces: number
  destAddress: string
  rejectionReason?: string
  createdAt: string
  approvedAt?: string
  refundPaymentId?: string
  refundReason?: string
}

export interface PortalWithdrawalRequestListResponse {
  items: PortalWithdrawalRequestItem[]
  total: number
}

// ── Deposit Addresses (Persistent Customer Wallets) ───────────────────

export interface PortalDepositAddressQuotaItem {
  networkId: string
  networkCode: string
  networkName: string
  used: number
  max: number
  tokens: string[]
}

export interface PortalDepositAddressItem {
  id: string
  customerId: string
  address: string
  networkId: string
  networkCode: string
  networkName: string
  tokens: string[]
  topUpCount: number
  lastTopUpAt?: string
  createdAt: string
  riskClass: string
  riskEnabled: boolean
  riskExpiresAt?: string
  riskAssets: AmlWalletAssetDisplay[]
  canRotate: boolean
  rotationBlockedReason: string
  nextRotationAt?: string
}

export interface PortalRotateDepositAddressRequest {
  reason: "aml_high_risk" | "customer_request"
}

export interface PortalRotateDepositAddressResponse extends PortalCreateDepositAddressResponse {
  rotationId: string
  oldWalletId: string
  oldAddress: string
  resourceVersion: number
  nextRotationAt: string
}

export interface PortalDepositAddressListResponse {
  items: PortalDepositAddressItem[]
  total: number
  quotas: PortalDepositAddressQuotaItem[]
}

export interface PortalCreateDepositAddressRequest {
  networkId: string
  customerId: string
}

export interface PortalCreateDepositAddressResponse {
  object: string
  id: string
  customerId: string
  address: string
  networkId: string
  networkCode: string
  networkName: string
  tokens: string[]
  reused: boolean
  createdAt: string
  qrCode?: string
}

export interface DepositAddressFilterParams {
  merchantId?: string
  networkId?: string
  search?: string
  limit?: number
  offset?: number
}

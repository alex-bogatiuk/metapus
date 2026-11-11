// types/merchant-api.ts
// TypeScript types for the Merchant Public API (/merchant/v1/).
// Must stay in sync with internal/infrastructure/http/v1/dto/merchant_public_api.go

export type InvoiceStatus =
  | 'created'
  | 'partially_paid'
  | 'paid'
  | 'confirmed'
  | 'completed'
  | 'underpaid'
  | 'expired'
  | 'cancelled'
  | 'reversed'

/** API key scope identifiers — must match Go merchant.APIKeyScope constants */
export type APIKeyScope =
  | 'invoice:create'
  | 'invoice:read'
  | 'address:create'
  | 'webhook:endpoint:read'
  | 'webhook:endpoint:write'
  | 'webhook:event:read'
  | 'webhook:event:redeliver'

export const API_KEY_SCOPES: { value: APIKeyScope; label: string }[] = [
  { value: 'invoice:create', label: 'Создание инвойсов' },
  { value: 'invoice:read', label: 'Чтение инвойсов' },
  { value: 'address:create', label: 'Создание кошельков' },
  { value: 'webhook:endpoint:read', label: 'Чтение webhook endpoints' },
  { value: 'webhook:endpoint:write', label: 'Управление webhook endpoints' },
  { value: 'webhook:event:read', label: 'Чтение webhook events' },
  { value: 'webhook:event:redeliver', label: 'Повторная доставка webhook events' },
]

// ─── Invoice DTOs ───────────────────────────────────────────────────────────

export interface MerchantInvoiceResponse {
  object: string
  id: string
  checkoutRef: string
  payUrl: string
  status: InvoiceStatus
  acceptsPayments: boolean
  resourceVersion: number
  expectedAmount: string
  receivedAmount: string
  confirmedAmount: string
  feeAmount: string
  creditedAmount: string
  netAmount: string
  overpaidAmount: string
  currency: string
  network: string
  decimalPlaces: number
  walletAddress: string
  requiredConfirmations: number
  finalityMode: string
  expiresAt: string
  orderId?: string
  externalId?: string
  customerId?: string
  description?: string
  notificationUrl?: string
  returnUrl?: string
  metadata: Record<string, string>
  paidAt?: string
  confirmedAt?: string
  completedAt?: string
  reversedAt?: string
  createdAt: string
  asOf: string
  chainObservedAt?: string
  payments: MerchantPaymentResponse[]
}

export interface MerchantInvoiceListResponse {
  object: string
  items: MerchantInvoiceResponse[]
  hasMore: boolean
  nextCursor?: string
}

export interface MerchantCurrencyItem {
  code: string
  symbol: string
  name: string
  network: string
  networkName: string
  standard: string
  decimalPlaces: number
  contractAddress?: string
  requiredConfirmations: number
  finalityMode: string
  acceptsPayments: boolean
}

export interface MerchantCurrencyListResponse {
  items: MerchantCurrencyItem[]
}

export interface MerchantPaymentResponse {
  id: string
  txHash: string
  eventKind: string
  /** Event index for eventKind; name retained for compatibility. */
  logIndex: number
  amount: string
  feeAmount: string
  netAmount: string
  status: string
  canonicality: string
  finalityStatus: string
  confirmations: number
  requiredConfirmations: number
  sourceNonce?: number
  sourceAddress?: string
  replacementTxHash?: string
  detectedAt: string
  confirmedAt?: string
}

export interface CreateMerchantInvoiceRequest {
  amount: string       // canonical raw units
  currency: string
  orderId?: string
  externalId?: string
  description?: string
  ttlMinutes?: number
  notificationUrl?: string
  returnUrl?: string
  metadata?: Record<string, string>
  customerId?: string
}

// ─── API Key DTOs ────────────────────────────────────────────────────────────

export interface MerchantAPIKeyListItem {
  id: string
  name: string
  keyPrefix: string
  scopes: APIKeyScope[]
  isActive: boolean
  createdByUserId?: string   // platform user who issued this key
  lastUsedAt?: string   // ISO 8601, optional
  expiresAt?: string   // ISO 8601, optional
  createdAt: string
}

export interface MerchantAPIKeyResponse extends MerchantAPIKeyListItem {
  /** Only set on key creation — never returned again. */
  plaintext?: string
}

export interface CreateMerchantAPIKeyRequest {
  name: string
  scopes?: APIKeyScope[]
  expiresAt?: string // ISO 8601
}

export interface MerchantAPIKeyListResponse {
  items: MerchantAPIKeyListItem[]
  total: number
}

// ─── Merchant User DTOs ──────────────────────────────────────────────────────
// Must stay in sync with dto.MerchantUserItem in merchant_public_api.go

/**
 * MerchantRole values — must match Go domain/catalogs/merchant.MerchantRole iota+1.
 * When adding a new role: add const here + label in MERCHANT_ROLES.
 */
export const MerchantRole = {
  Owner: 1,
  Manager: 2,
  Viewer: 3,
} as const
export type MerchantRole = typeof MerchantRole[keyof typeof MerchantRole]

export const MERCHANT_ROLES: { value: MerchantRole; label: string; description: string }[] = [
  { value: MerchantRole.Owner, label: 'Владелец', description: 'Полный доступ, управление ключами и пользователями' },
  { value: MerchantRole.Manager, label: 'Менеджер', description: 'Просмотр документов и операций, без настроек' },
  { value: MerchantRole.Viewer, label: 'Наблюдатель', description: 'Только чтение платёжных документов' },
]

export interface MerchantUserItem {
  userId: string
  merchantId: string
  role: MerchantRole
  roleName: string
  createdAt: string // ISO 8601
  userEmail?: string // populated by backend JOIN
  userFullName?: string // populated by backend JOIN
}

export interface AddMerchantUserRequest {
  userId: string
  role: MerchantRole
}

export interface UpdateMerchantUserRoleRequest {
  role: MerchantRole
}

export interface MerchantUserListResponse {
  items: MerchantUserItem[]
  total: number
}

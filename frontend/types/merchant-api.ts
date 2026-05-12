// types/merchant-api.ts
// TypeScript types for the Merchant Public API (/merchant/v1/).
// Must stay in sync with internal/infrastructure/http/v1/dto/merchant_public_api.go

export type InvoiceStatus =
  | 'created'
  | 'partially_paid'
  | 'paid'
  | 'overpaid'
  | 'confirmed'
  | 'expired'
  | 'cancelled';

/** API key scope identifiers — must match Go merchant.APIKeyScope constants */
export type APIKeyScope = 'invoice:create' | 'invoice:read';

export const API_KEY_SCOPES: { value: APIKeyScope; label: string }[] = [
  { value: 'invoice:create', label: 'Создание инвойсов' },
  { value: 'invoice:read',   label: 'Чтение инвойсов' },
];

// ─── Invoice DTOs ───────────────────────────────────────────────────────────

export interface MerchantInvoiceResponse {
  id:            string;
  status:        InvoiceStatus;
  amount:        number;       // minor units (int64)
  currency:      string;       // e.g. "USDT_TRC20"
  network:       string;       // e.g. "TRON"
  walletAddress: string;
  expiresAt:     string;       // ISO 8601
  orderId?:      string;
  description?:  string;
  createdAt:     string;       // ISO 8601
}

export interface CreateMerchantInvoiceRequest {
  amount:        number;       // minor units (int64)
  currency:      string;
  orderId?:      string;
  description?:  string;
  ttlMinutes?:   number;
  callbackUrl?:  string;
  customerEmail?: string;
}

// ─── API Key DTOs ────────────────────────────────────────────────────────────

export interface MerchantAPIKeyListItem {
  id:                string;
  name:              string;
  keyPrefix:         string;
  scopes:            APIKeyScope[];
  isActive:          boolean;
  createdByUserId?:  string;   // platform user who issued this key
  lastUsedAt?:       string;   // ISO 8601, optional
  expiresAt?:        string;   // ISO 8601, optional
  createdAt:         string;
}

export interface MerchantAPIKeyResponse extends MerchantAPIKeyListItem {
  /** Only set on key creation — never returned again. */
  plaintext?: string;
}

export interface CreateMerchantAPIKeyRequest {
  name:       string;
  scopes?:    APIKeyScope[];
  expiresAt?: string; // ISO 8601
}

export interface MerchantAPIKeyListResponse {
  items: MerchantAPIKeyListItem[];
  total: number;
}

// ─── Merchant User DTOs ──────────────────────────────────────────────────────
// Must stay in sync with dto.MerchantUserItem in merchant_public_api.go

/**
 * MerchantRole values — must match Go domain/catalogs/merchant.MerchantRole iota+1.
 * When adding a new role: add const here + label in MERCHANT_ROLES.
 */
export const MerchantRole = {
  Owner:   1,
  Manager: 2,
  Viewer:  3,
} as const;
export type MerchantRole = typeof MerchantRole[keyof typeof MerchantRole];

export const MERCHANT_ROLES: { value: MerchantRole; label: string; description: string }[] = [
  { value: MerchantRole.Owner,   label: 'Владелец',   description: 'Полный доступ, управление ключами и пользователями' },
  { value: MerchantRole.Manager, label: 'Менеджер',   description: 'Просмотр документов и операций, без настроек' },
  { value: MerchantRole.Viewer,  label: 'Наблюдатель', description: 'Только чтение платёжных документов' },
];

export function merchantRoleLabel(role: MerchantRole): string {
  return MERCHANT_ROLES.find((r) => r.value === role)?.label ?? String(role);
}

export interface MerchantUserItem {
  userId:       string;
  merchantId:   string;
  role:         MerchantRole;
  roleName:     string;
  createdAt:    string; // ISO 8601
  userEmail?:   string; // populated by backend JOIN
  userFullName?: string; // populated by backend JOIN
}

export interface AddMerchantUserRequest {
  userId: string;
  role:   MerchantRole;
}

export interface UpdateMerchantUserRoleRequest {
  role: MerchantRole;
}

export interface MerchantUserListResponse {
  items: MerchantUserItem[];
  total: number;
}

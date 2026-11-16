export type WalletPoolHealth = "healthy" | "low" | "depleted" | "paused"
export type ChainFamily = "tron" | "evm" | "ton" | "solana"
export type WalletProvisioningMode =
  | "unsupported"
  | "derived"
  | "derived_with_activation"

export interface WalletPoolActivationPolicy {
  maxConcurrent: number
  reserveMinor: string
  minHotBalanceMinor: string
  dailyBudgetMinor: string
}

export interface WalletPoolCapabilities {
  family: ChainFamily
  walletProvisioning: WalletProvisioningMode
}

export interface WalletPoolPolicy {
  networkId: string
  networkCode: string
  networkName: string
  chainFamily: ChainFamily
  nativeTokenSymbol: string
  nativeDecimals: number
  enabled: boolean
  lowWatermark: number
  targetReady: number
  maxTotalWallets: number
  maxProvisionBatch: number
  maxActiveInvoiceLeasesPerMerchant: number
  maxPersistentAddressesPerMerchant: number
  maxLeaseStartsPerMinutePerMerchant: number
  minGlobalReadyReserve: number
  activation: WalletPoolActivationPolicy | null
  retryAfterSeconds: number
  addressReuseCooldownSeconds: number
  maxWatcherLagSeconds: number
  leaseBlockedReason?: string
  leaseBlockedAt?: string
  derivationNamespace: string
  needsReview: boolean
  version: number
  updatedAt: string
  updatedBy: string
  lastReconciledAt?: string
  lastErrorCode?: string
  lastErrorMessage?: string
}

export interface WalletPoolStats {
  ready: number
  cooldown: number
  leased: number
  assigned: number
  provisioning: number
  activating: number
  sweepPending: number
  held: number
  failed: number
  total: number
  inboxUnmatched: number
  inboxNeedsReview: number
  inboxPoison: number
}

export interface WalletPoolState {
  policy: WalletPoolPolicy
  capabilities: WalletPoolCapabilities
  stats: WalletPoolStats
  state: WalletPoolHealth
}

export interface WalletPoolListResponse {
  items: WalletPoolState[]
}

export interface WalletPoolPolicyPatch {
  enabled: boolean
  lowWatermark: number
  targetReady: number
  maxTotalWallets: number
  maxProvisionBatch: number
  maxActiveInvoiceLeasesPerMerchant: number
  maxPersistentAddressesPerMerchant: number
  maxLeaseStartsPerMinutePerMerchant: number
  minGlobalReadyReserve: number
  activation?: WalletPoolActivationPolicy
  retryAfterSeconds: number
  addressReuseCooldownSeconds?: number
  maxWatcherLagSeconds?: number
  version: number
}

export interface WalletPoolPlanResult {
  networkId: string
  ready: number
  inFlight: number
  total: number
  created: number
}

export interface WalletPoolReconcileResponse {
  accepted: boolean
  results: WalletPoolPlanResult[]
  state: WalletPoolState
}

export interface MerchantWalletAllocationLimits {
  merchantId: string
  networkId: string
  maxActiveInvoiceLeases: number | null
  maxPersistentAddresses: number | null
  maxLeaseStartsPerMinute: number | null
  effectiveMaxActiveInvoiceLeases: number
  effectiveMaxPersistentAddresses: number
  effectiveMaxLeaseStartsPerMinute: number
  version: number
  updatedAt?: string
  updatedBy?: string
}

export interface MerchantWalletAllocationLimitsUpdate {
  maxActiveInvoiceLeases: number | null
  maxPersistentAddresses: number | null
  maxLeaseStartsPerMinute: number | null
  version: number
}

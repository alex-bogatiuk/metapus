export type ChainFamily = "tron" | "evm" | "ton" | "solana"
export type ResourceStrategy = "rental_required" | "prefer_rental" | "burn_only"
export type FeeFundingMode = "source_only" | "jit"
export type FinalityMode =
  | "confirmations"
  | "protocol_finalized"
  | "confirmations_or_finalized"

export type SequenceModel = "none" | "account"
export type CheckpointTimeUnit = "unix_millis" | "unix_seconds"
export type TokenAccountModel = "same_address" | "derived_per_asset"

export interface ChainFamilyCapabilities {
  family: ChainFamily
  walletProvisioning: "derived" | "derived_with_activation"
  requiredRpcRoles: string[]
  externalResourceMode: "none" | "optional"
  sequenceModel: SequenceModel
  checkpointTimeUnit: CheckpointTimeUnit
  tokenAccountModel: TokenAccountModel
  minWatcherLagSeconds: number
  supportsReplacement: boolean
}

export interface CryptoNetworkRef {
  id: string
  code: string
  family: ChainFamily
  networkIdentifier: string
  nativeTokenId: string
  nativeTokenSymbol: string
  nativeDecimals: number
  requiredConfirmations: number
  finalityMode: FinalityMode
}

export interface FeeFundingPolicy {
  mode: FeeFundingMode
  dailyBudgetMinor: string
  minFeeWalletBalanceMinor: string
  minSourceWalletResidualMinor: string
  maxFundingAmountMinor: string
}

export interface ExternalResourcePolicy {
  strategy: ResourceStrategy
  providerAccountIds: string[]
  providerFailureThreshold: number
  providerOpenSeconds: number
}

export type CryptoProviderAccount = import("@/configuration/crypto/types/generated/http-contracts").CryptoProviderAccountResponse

export interface CryptoNetworkPolicy {
  networkId: string
  enabled: boolean
  perTransactionMaxFeeMinor: string
  dailyNetworkFeeBudgetMinor: string
  feeFunding: FeeFundingPolicy
  externalResources?: ExternalResourcePolicy
  retryDelaySeconds: number
  rpcFailureThreshold: number
  rpcOpenSeconds: number
  version: number
  updatedBy: string
  createdAt: string
  updatedAt: string
}

export interface CryptoRuntimeEndpoint {
	endpointId?: string
	appliedRevision?: string
	operations?: { operation: string; state: string; failures: number; reasonCode?: string; retryAt?: string | null }[]
  providerId: string
  role: string
  state:
  | "closed"
  | "open"
  | "half_open"
  | "degraded"
  | "lagging"
  | "invalid"
  | "wrong_chain"
  failures: number
  lastError?: string
}

export interface CryptoNetworkRuntime {
  networkId: string
  rpcEndpoints: CryptoRuntimeEndpoint[]
  signerReady: boolean
  signerError?: string
  resourceProviderReady: boolean
  resourceProviderState: string
  resourceProviders?: CryptoRuntimeEndpoint[]
  observedAt: string
  workerId?: string
  operationalHold: boolean
  operationalHoldReason?: string
  /** The worker's last comparison of each watcher stream with the chain head. */
  ingestion?: CryptoNetworkIngestion
}

export type CryptoNetworkIngestion = import("@/configuration/crypto/types/generated/http-contracts").CryptoNetworkIngestionStatus
export type CryptoIngestionStream = import("@/configuration/crypto/types/generated/http-contracts").CryptoIngestionStreamStatus

export interface CryptoNetworkState {
  network: CryptoNetworkRef
  /** The worker reads only watched networks; processing requires watching. */
  watching: boolean
  capabilities: ChainFamilyCapabilities
  policy: CryptoNetworkPolicy
  runtime: CryptoNetworkRuntime
}

export interface CryptoNetworkListResponse {
  items: CryptoNetworkState[]
}

export interface CryptoNetworkPolicyPatch {
  version: number
  enabled: boolean
  perTransactionMaxFeeMinor: string
  dailyNetworkFeeBudgetMinor: string
  feeFunding: FeeFundingPolicy
  externalResources?: ExternalResourcePolicy
  clearExternalResources: boolean
  retryDelaySeconds: number
  rpcFailureThreshold: number
  rpcOpenSeconds: number
}

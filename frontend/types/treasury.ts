import type { JsonObject } from "@/lib/json-value"
import type { RefDisplay } from "@/types/common"

export type TreasuryWithdrawalStatus =
  | "pending_signature"
  | "pending_approval"
  | "approved"
  | "signing"
  | "broadcast"
  | "confirmed"
  | "held"
  | "rejected"
  | "failed"

export interface TreasuryAddress {
  id: string
  networkId: string
  address: string
  label: string
  isActive: boolean
}

export interface TreasuryWithdrawal {
  id: string
  number: string
  date: string
  tokenId: string
  sourceWalletId: string
  addressId: string
  destAddress: string
  amount: string
  networkFee: string
  txHash: string
  status: TreasuryWithdrawalStatus
  approvedBy?: string
  approvedAt?: string
  description?: string
  createdBy?: string
  createdAt: string
  updatedAt: string
  token?: RefDisplay
  sourceWallet?: RefDisplay
}

export interface TreasuryAvailability {
  tokenId: string
  amount: string
  reserved: string
  available: string
}

export interface TreasurySourceWallet {
  id: string
  networkId: string
  name: string
  address: string
  ledgerAmount: string
  reservedAmount: string
  availableAmount: string
  lastChainAmount?: string
  lastReconciliationAt?: string
}

export interface ExternalOutflow {
  id: string
  networkId: string
  tokenId?: string
  sourceWalletId?: string
  txHash: string
  fromAddress: string
  toAddress: string
  contractAddress: string
  amount: string
  blockNumber: number
  blockHash: string
  confirmations: number
  requiredConfirmations: number
  eventKind: string
  eventIndex: number
  finalityStatus: string
  status: "pending_review" | "classified" | "resolved"
  classification?: ExternalOutflowClassification
  detectedAt: string
}

export type ExternalOutflowClassification =
  | "matched_basis"
  | "unauthorized_loss"
  | "false_positive_or_reorg"

export interface ExternalOutflowAction {
  actorType: string
  actorId: string
  action: string
  oldState: string
  newState: string
  reason: string
  evidence?: JsonObject
  createdAt: string
}

export interface ExternalOutflowHold {
  scope: string
  reason: string
  ownerType: string
  ownerId: string
  heldAt: string
}

export interface CryptoCustodyAdjustment {
  id: string
  externalOutflowId: string
  networkId: string
  walletId: string
  tokenId: string
  amount: string
  status: "pending_approval" | "posted" | "rejected"
  makerId: string
  decisionBy?: string
  decisionReason?: string
  postedVersion: number
  createdAt: string
  decidedAt?: string
}

export interface ExternalOutflowDetail {
  outflow: ExternalOutflow
  classificationReason: string
  classificationEvidence?: JsonObject
  matchedBasisType: string
  matchedBasisId: string
  adjustment?: CryptoCustodyAdjustment
  openHolds: ExternalOutflowHold[]
  actions: ExternalOutflowAction[]
  resolution: string
  canResolve: boolean
  resolutionBlockers: string[]
}

export interface ClassifyExternalOutflowRequest {
  classification: ExternalOutflowClassification
  matchedBasisType?: string
  matchedBasisId?: string
  reason: string
  evidence?: JsonObject
}

export interface TreasuryTokenOption {
  id: string
  code: string
  name: string
  symbol?: string
  networkId: string
  decimalPlaces: number
  standard?: string
  deletionMark?: boolean
}

export interface TreasuryWalletOption {
  id: string
  code: string
  name: string
  networkId: string
  address: string
  tier: string
  isActive: boolean
  deletionMark?: boolean
}

export interface CreateTreasuryWithdrawalRequest {
  tokenId: string
  sourceWalletId: string
  addressId: string
  destAddress: string
  amount: string
  description?: string
}

export interface UpdateTreasuryWithdrawalRequest {
  description?: string
}

export interface CreateTreasuryAddressRequest {
  networkId: string
  address: string
  label?: string
}

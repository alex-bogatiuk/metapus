import type { JsonObject } from "@/lib/json-value"
export type WithdrawalRequestStatus =
  | "pending_signature"
  | "pending_approval"
  | "approved"
  | "signing"
  | "broadcast"
  | "confirmed"
  | "held"
  | "rejected"
  | "failed"
  | "partially_completed"

export interface AdminWithdrawalRequestItem {
 riskClass?: string
 riskAccepted: boolean
 sources?: import("@/types/generated/http-contracts").PortalWithdrawalSource[]
  id: string
  number: string
  date: string
  status: WithdrawalRequestStatus
  merchantId: string
  merchantName: string
  tokenId: string
  symbol: string
  network: string
  amount: string
  decimalPlaces: number
  destAddress: string
  approvedById?: string
  approvedByName?: string
  approvedAt?: string
  rejectionReason?: string
  withdrawalId?: string
  createdAt: string
}

export interface AdminWithdrawalRequestDetail extends AdminWithdrawalRequestItem {
  deletionMark: boolean
  version: number
  attributes: JsonObject
  approvedBy?: string
  valuation?: WithdrawalRequestUSDValuation
  updatedAt: string
  createdBy?: string
  updatedBy?: string
  date: string
  posted: boolean
  postedVersion: number
  basisType?: string
  basisId?: string
  description?: string
  addressId?: string
}

export interface AdminWithdrawalRequestListResponse {
  items: AdminWithdrawalRequestItem[]
  total: number
  nextCursor?: string
  prevCursor?: string
  hasMore: boolean
  hasPrev: boolean
  targetIndex?: number
  totalCount: number | null
}

export interface WithdrawalRequestUSDValuation {
  status: string
  amount: string | null
  rate?: string
  multiplier: number
  source?: string
  rateAsOf?: string
  evaluatedAt: string
}

export interface AdminApproveWithdrawalResponse {
  status: WithdrawalRequestStatus
}

export interface AdminWithdrawalRequestFilter {
  status?: WithdrawalRequestStatus | "active" | "all"
  limit?: number
}

export type InboundReviewStatus = "needs_review" | "resolved"
export type InboundReviewOutcome = "credited_manually" | "refunded_manually" | "not_attributable"

export interface InboundReviewItem {
  id: string
  invoiceId: string
  merchantId: string
  networkId: string
  tokenId: string
  txHash: string
  amount: string
  fromAddress: string
  toAddress: string
  invoiceStatus: string
  status: InboundReviewStatus
  reviewOutcome: InboundReviewOutcome | ""
  reviewReference: string
  reviewNote: string
  reviewedBy: string
  createdAt: string
  reviewedAt: string | null
  canResolve: boolean
  matchReason: string
  customerId: string
}

export interface InboundReviewList {
  items: InboundReviewItem[]
  total: number
}

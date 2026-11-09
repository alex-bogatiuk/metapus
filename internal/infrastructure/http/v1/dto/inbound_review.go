package dto

import "time"

type InboundReviewItem struct {
	ID              string     `json:"id"`
	InvoiceID       string     `json:"invoiceId"`
	MerchantID      string     `json:"merchantId"`
	NetworkID       string     `json:"networkId"`
	TokenID         string     `json:"tokenId"`
	TxHash          string     `json:"txHash"`
	Amount          string     `json:"amount"`
	FromAddress     string     `json:"fromAddress"`
	ToAddress       string     `json:"toAddress"`
	InvoiceStatus   string     `json:"invoiceStatus"`
	Status          string     `json:"status"`
	ReviewOutcome   string     `json:"reviewOutcome"`
	ReviewReference string     `json:"reviewReference"`
	ReviewNote      string     `json:"reviewNote"`
	ReviewedBy      string     `json:"reviewedBy"`
	CreatedAt       time.Time  `json:"createdAt"`
	ReviewedAt      *time.Time `json:"reviewedAt"`
	CanResolve      bool       `json:"canResolve"`
	MatchReason     string     `json:"matchReason"`
	CustomerID      string     `json:"customerId"`
}

type InboundReviewListResponse struct {
	Items []InboundReviewItem `json:"items"`
	Total int                 `json:"total"`
}

type ResolveInboundReviewRequest struct {
	Outcome   string `json:"outcome" binding:"required"`
	Reference string `json:"reference"`
	Note      string `json:"note" binding:"required"`
}

type ResolveInboundReviewResponse struct {
	Resolved bool `json:"resolved"`
}

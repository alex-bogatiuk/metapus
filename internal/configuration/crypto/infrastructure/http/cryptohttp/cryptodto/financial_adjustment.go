package cryptodto

import (
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

type ProposeFinancialAdjustmentRequest struct {
	Mode                  string                             `json:"mode" binding:"required"`
	PaymentID             string                             `json:"paymentId,omitempty"`
	ExpectedPostedVersion int                                `json:"expectedPostedVersion,omitempty"`
	WalletID              string                             `json:"walletId,omitempty"`
	TokenID               string                             `json:"tokenId,omitempty"`
	Direction             string                             `json:"direction,omitempty"`
	Amount                string                             `json:"amount,omitempty"`
	SourceLines           []FinancialAdjustmentSourceRequest `json:"sourceLines,omitempty"`
	ReasonCode            string                             `json:"reasonCode" binding:"required"`
	Evidence              json.RawMessage                    `json:"evidence" binding:"required"`
}

type FinancialAdjustmentSourceRequest struct {
	Register string `json:"register" binding:"required"`
	LineID   string `json:"lineId" binding:"required"`
	Amount   string `json:"amount" binding:"required"`
}

type FinancialAdjustmentSourceResponse struct {
	Register  string `json:"register"`
	LineID    string `json:"lineId"`
	Amount    string `json:"amount"`
	Reversed  string `json:"reversed"`
	Available string `json:"available"`
	TokenID   string `json:"tokenId"`
	Dimension string `json:"dimension"`
}

type FinancialAdjustmentPaymentSourcesResponse struct {
	PaymentID     string                              `json:"paymentId"`
	PostedVersion int                                 `json:"postedVersion"`
	Lines         []FinancialAdjustmentSourceResponse `json:"lines"`
}

type FinancialAdjustmentResponse struct {
	ID             string                             `json:"id"`
	PaymentID      string                             `json:"paymentId,omitempty"`
	PostedVersion  int                                `json:"postedVersion"`
	Mode           string                             `json:"mode"`
	BasisKind      string                             `json:"basisKind"`
	WalletID       string                             `json:"walletId,omitempty"`
	TokenID        string                             `json:"tokenId,omitempty"`
	Direction      string                             `json:"direction,omitempty"`
	Amount         string                             `json:"amount,omitempty"`
	DecisionMode   string                             `json:"decisionMode"`
	SourceLines    []FinancialAdjustmentSourceRequest `json:"sourceLines,omitempty"`
	ReasonCode     string                             `json:"reasonCode"`
	Evidence       json.RawMessage                    `json:"evidence"`
	MakerID        string                             `json:"makerId"`
	CheckerID      *string                            `json:"checkerId,omitempty"`
	Status         string                             `json:"status"`
	DecisionReason string                             `json:"decisionReason"`
	CreatedAt      string                             `json:"createdAt"`
	DecidedAt      *string                            `json:"decidedAt,omitempty"`
}

type FinancialAdjustmentListResponse struct {
	Items []FinancialAdjustmentResponse `json:"items"`
}

func FromFinancialAdjustment(value *cryptoapp.FinancialAdjustment) FinancialAdjustmentResponse {
	result := FinancialAdjustmentResponse{
		ID: value.ID.String(), Mode: value.Mode, DecisionMode: value.DecisionMode,
		BasisKind:  value.BasisKind,
		ReasonCode: value.ReasonCode,
		Evidence:   value.Evidence, MakerID: value.MakerID.String(),
		Status: value.Status, DecisionReason: value.DecisionReason,
		CreatedAt: value.CreatedAt.Format(time.RFC3339Nano),
	}
	if value.PaymentID != (id.ID{}) {
		result.PaymentID = value.PaymentID.String()
		result.PostedVersion = value.PostedVersion
	}
	if value.WalletID != nil {
		result.WalletID = value.WalletID.String()
	}
	if value.TokenID != nil {
		result.TokenID = value.TokenID.String()
	}
	result.Direction = value.Direction
	result.Amount = value.Amount
	if value.CheckerID != nil {
		checkerID := value.CheckerID.String()
		result.CheckerID = &checkerID
	}
	if value.DecidedAt != nil {
		decidedAt := value.DecidedAt.Format(time.RFC3339Nano)
		result.DecidedAt = &decidedAt
	}
	return result
}

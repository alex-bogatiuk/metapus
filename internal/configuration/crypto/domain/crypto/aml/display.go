package aml

import (
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

// AssessmentDisplay describes evidence for an operator; it is not a permit.
// A bootstrap assessment must never be presented as a provider check.
type AssessmentDisplay struct {
	Class       Class      `json:"riskClass"`
	Enabled     bool       `json:"enabled"`
	Source      string     `json:"source"`
	Reason      string     `json:"reason"`
	ScoreBP     *int       `json:"scoreBp,omitempty"`
	EvaluatedAt *time.Time `json:"evaluatedAt,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
}

type WalletAssetDisplay struct {
	TokenID          id.ID             `json:"tokenId"`
	Symbol           string            `json:"symbol"`
	HasFunds         bool              `json:"hasFunds"`
	InventoryCovered bool              `json:"inventoryCovered"`
	Assessment       AssessmentDisplay `json:"assessment"`
	BatchID          *id.ID            `json:"batchId,omitempty"`
	IntakeClass      string            `json:"intakeClass,omitempty"`
	BatchState       string            `json:"batchState,omitempty"`
}

type WalletDisplay struct {
	Class     Class
	ExpiresAt *time.Time
	Enabled   bool
	HasFunds  bool
	Assets    []WalletAssetDisplay
}

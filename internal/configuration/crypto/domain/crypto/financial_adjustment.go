package crypto

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"time"
)

// FinancialAdjustment corrects payment source lines or records an independently
// reviewed wallet variance. Its derived basis, evidence and source selection are
// frozen at proposal time; free-form evidence grants no chain ownership.
type FinancialAdjustment struct {
	ID             id.ID           `db:"id" json:"id"`
	PaymentID      id.ID           `db:"payment_id" json:"paymentId"`
	PostedVersion  int             `db:"posted_version" json:"postedVersion"`
	Mode           string          `db:"mode" json:"mode"`
	BasisKind      string          `db:"basis_kind" json:"basisKind"`
	WalletID       *id.ID          `db:"wallet_id" json:"walletId,omitempty"`
	TokenID        *id.ID          `db:"token_id" json:"tokenId,omitempty"`
	Direction      string          `db:"direction" json:"direction,omitempty"`
	Amount         string          `db:"amount" json:"amount,omitempty"`
	DecisionMode   string          `db:"decision_mode" json:"decisionMode"`
	ReasonCode     string          `db:"reason_code" json:"reasonCode"`
	Evidence       json.RawMessage `db:"evidence" json:"evidence"`
	MakerID        id.ID           `db:"maker_id" json:"makerId"`
	CheckerID      *id.ID          `db:"checker_id" json:"checkerId,omitempty"`
	Status         string          `db:"status" json:"status"`
	DecisionReason string          `db:"decision_reason" json:"decisionReason,omitempty"`
	CreatedAt      time.Time       `db:"created_at" json:"createdAt"`
	DecidedAt      *time.Time      `db:"decided_at" json:"decidedAt,omitempty"`
}

// AdjustmentPaymentBasis is loaded and locked by the persistence adapter.
type AdjustmentPaymentBasis struct {
	Status  string
	Posted  bool
	Version int
}

// AdjustmentPostingPlan describes the decided business effect, not SQL.
type AdjustmentPostingPlan struct {
	Adjustment        FinancialAdjustment
	ActorID           id.ID
	Reason            string
	CompensatePayment bool
	VarianceType      string
}

func (a FinancialAdjustment) CanDecide(actor id.ID, direct bool) error {
	if a.Status != "pending_approval" {
		return apperror.NewConflict("crypto adjustment decision is already final")
	}
	expected := "two_person"
	if direct {
		expected = "direct"
	}
	if a.DecisionMode != expected {
		return apperror.NewConflict("adjustment approval route changed")
	}
	if !direct && a.MakerID == actor {
		return apperror.NewForbidden("adjustment maker cannot approve or reject it")
	}
	return nil
}
func (a FinancialAdjustment) PlanPosting(actor id.ID, reason string, direct bool, basis AdjustmentPaymentBasis) (AdjustmentPostingPlan, error) {
	if err := a.CanDecide(actor, direct); err != nil {
		return AdjustmentPostingPlan{}, err
	}
	plan := AdjustmentPostingPlan{Adjustment: a, ActorID: actor, Reason: reason}
	if a.Mode == "payment_reversal" {
		if basis.Status != "settled" || !basis.Posted || basis.Version != a.PostedVersion {
			return plan, apperror.NewConflict("payment generation changed before adjustment approval")
		}
		plan.CompensatePayment = true
	} else {
		if a.Mode != "wallet_balance" || a.WalletID == nil || a.TokenID == nil || (a.Direction != "receipt" && a.Direction != "expense") {
			return plan, apperror.NewConflict("wallet correction basis is missing")
		}
		plan.VarianceType = "shortage"
		if a.Direction == "receipt" {
			plan.VarianceType = "surplus"
		}
	}
	return plan, nil
}

func (b AdjustmentPaymentBasis) ValidateAdjustmentGeneration(expected int) error {
	if b.Status != "settled" || !b.Posted || b.Version < 1 || (expected > 0 && expected != b.Version) {
		return apperror.NewConflict("payment generation is not settled and posted")
	}
	return nil
}

package aml

import (
	"strings"
	"time"

	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type BatchPolicyRequest struct {
	MerchantID, NetworkID, TokenID id.ID
	Class                          Class
	ExpectedVersion                int64
	MaxGross                       types.RawAmount
	MaxAgeSeconds                  int
	Actor                          string
}

func (r BatchPolicyRequest) Validate() error {
	if id.IsNil(r.MerchantID) || id.IsNil(r.NetworkID) || id.IsNil(r.TokenID) || (r.Class != Low && r.Class != Medium) ||
		r.ExpectedVersion < 0 || !r.MaxGross.IsPositive() || r.MaxAgeSeconds < 1 || r.MaxAgeSeconds > 2592000 || strings.TrimSpace(r.Actor) == "" {
		return apperror.NewValidation("batch policy requires owned low/medium scope, exact positive cap, age and actor")
	}
	return nil
}

type OpenBatchRequest struct {
	ID, MerchantID, NetworkID, TokenID, CollectorID id.ID
	Class                                           Class
	PolicyVersion, ScopeVersion                     int64
	Actor                                           string
}

func (r OpenBatchRequest) Validate() error {
	if id.IsNil(r.ID) || id.IsNil(r.MerchantID) || id.IsNil(r.NetworkID) || id.IsNil(r.TokenID) || id.IsNil(r.CollectorID) ||
		(r.Class != Low && r.Class != Medium) || r.PolicyVersion <= 0 || r.ScopeVersion <= 0 || strings.TrimSpace(r.Actor) == "" {
		return apperror.NewValidation("batch requires idempotent identity, owned collector and current policy/scope")
	}
	return nil
}

type Batch struct {
	ID, CollectorID                          id.ID
	Revision, ExecutionRevision              int64
	MaxGross, Committed, Reserved, Unplanned types.RawAmount
	IntakeClosesAt                           time.Time
	IntakeClosedAt                           *time.Time
	ExecutionHeld                            bool
}

// AvailableCapacity is lifetime controlled intake. An outgoing transfer does
// not replenish capacity; an uncertain incoming continues to occupy its cap.
func (b Batch) AvailableCapacity() types.RawAmount {
	used := b.Committed.Add(b.Reserved).Add(b.Unplanned)
	if used.Cmp(b.MaxGross) >= 0 {
		return types.RawAmount{}
	}
	return b.MaxGross.Sub(used)
}

type BatchSweepRequest struct {
	SweepAttemptID, MerchantID, NetworkID, TokenID, SourceWalletID id.ID
	Amount                                                         types.RawAmount
	SourceRevision, RiskRevision, ScopeVersion                     int64
	Class                                                          Class
}

func (r BatchSweepRequest) Validate() error {
	if id.IsNil(r.SweepAttemptID) || id.IsNil(r.MerchantID) || id.IsNil(r.NetworkID) || id.IsNil(r.TokenID) || id.IsNil(r.SourceWalletID) ||
		!r.Amount.IsPositive() || r.SourceRevision <= 0 || r.RiskRevision <= 0 || r.ScopeVersion <= 0 || (r.Class != Low && r.Class != Medium) {
		return apperror.NewValidation("batch sweep requires owned scope, positive amount and current source/risk revisions")
	}
	return nil
}

package aml

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

// MerchantBalanceEvidence is a single consistent financial/risk read snapshot.
// Source amounts are net merchant principal, never gross chain inventory.
type MerchantBalanceEvidence struct {
	Scope           BalanceScope
	Symbol          string
	Network         string
	DecimalPlaces   int
	RoutingEnabled  bool
	ScopeVersion    int64
	Ledger          types.CryptoAmount
	LedgerMatches   bool
	PendingChain    types.CryptoAmount
	HasHighExposure bool
	Sources         []PrincipalPosition
	Commitments     []DebitCommitment
}

// PrincipalPosition is admitted only when its financial allocation is proven.
// Historical internal transfers need explicit allocations; an equal sum of
// incoming merchant credits is not proof of the remaining net principal.
type PrincipalPosition struct {
	WalletID     id.ID
	Amount       types.CryptoAmount
	Class        Class
	Revision     int64
	RiskRevision int64
	ScopeVersion int64
	Covered      bool
	ExpiresAt    *time.Time
	Held         bool
}

// DebitCommitment is the still-unfulfilled ledger debit, net of compensation.
// Source reservations are not another merchant debit and are never added here.
type DebitCommitment struct {
	RequestID  id.ID
	Amount     types.CryptoAmount
	Executable bool // held/signing/unknown outcome remain executable
}

type MerchantRiskBalance struct {
	MerchantID         id.ID              `json:"merchantId"`
	NetworkID          id.ID              `json:"networkId"`
	TokenID            id.ID              `json:"tokenId"`
	Symbol             string             `json:"symbol"`
	Network            string             `json:"network"`
	DecimalPlaces      int                `json:"decimalPlaces"`
	RoutingEnabled     bool               `json:"routingEnabled"`
	ScopeVersion       int64              `json:"scopeVersion"`
	AllocationComplete bool               `json:"allocationComplete"`
	Reason             string             `json:"reason"`
	HasHighExposure    bool               `json:"hasHighExposure"`
	LedgerBalance      types.CryptoAmount `json:"ledgerBalance"`
	PendingChain       types.CryptoAmount `json:"pendingChain"`
	PendingRestoration types.CryptoAmount `json:"pendingRestoration"`
	Total              types.CryptoAmount `json:"total"`
	Free               types.CryptoAmount `json:"free"`
	Reserved           types.CryptoAmount `json:"reserved"`
	TechnicalHold      types.CryptoAmount `json:"technicalHold"`
	Buckets            []BalanceBucket    `json:"buckets"`
}

type MerchantBalanceSnapshot struct {
	SnapshotID string                `json:"snapshotId"`
	AsOf       time.Time             `json:"asOf"`
	FreshUntil time.Time             `json:"freshUntil"`
	Items      []MerchantRiskBalance `json:"items"`
}

type MerchantBalanceRepository interface {
	ReadMerchantRiskBalance(context.Context, id.ID) (time.Time, []MerchantBalanceEvidence, error)
}

// BuildMerchantRiskBalance partitions one claim exactly once. This is an
// observation, not a spend permit. Unknown attribution never invents a free
// low source or erases an independently known high exposure.
func BuildMerchantRiskBalance(e MerchantBalanceEvidence, now time.Time) (MerchantRiskBalance, error) {
	if id.IsNil(e.Scope.MerchantID) || id.IsNil(e.Scope.NetworkID) || id.IsNil(e.Scope.TokenID) {
		return MerchantRiskBalance{}, apperror.NewValidation("merchant, network and token are required for risk balance")
	}
	r := MerchantRiskBalance{MerchantID: e.Scope.MerchantID, NetworkID: e.Scope.NetworkID, TokenID: e.Scope.TokenID,
		Symbol: e.Symbol, Network: e.Network, DecimalPlaces: e.DecimalPlaces, RoutingEnabled: e.RoutingEnabled,
		ScopeVersion: e.ScopeVersion, LedgerBalance: e.Ledger, PendingChain: e.PendingChain, HasHighExposure: e.HasHighExposure}
	seen := make(map[id.ID]bool, len(e.Commitments))
	unbackedExecution := false
	for _, c := range e.Commitments {
		if id.IsNil(c.RequestID) || seen[c.RequestID] {
			return r, apperror.NewValidation("invalid or duplicate debit commitment")
		}
		seen[c.RequestID] = true
		if c.Executable {
			unbackedExecution = unbackedExecution || c.Amount.IsZero()
			r.Reserved = r.Reserved.Add(c.Amount)
		} else {
			r.PendingRestoration = r.PendingRestoration.Add(c.Amount)
		}
	}
	r.Total = e.Ledger.Add(r.Reserved).Add(r.PendingRestoration)
	positions := make([]BalancePosition, 0, len(e.Sources)+1)
	attributed := types.ZeroCryptoAmount()
	allocationCovered := true
	clear(seen)
	for _, s := range e.Sources {
		if id.IsNil(s.WalletID) || seen[s.WalletID] || !s.Class.Valid() {
			return r, apperror.NewValidation("invalid or duplicate principal position")
		}
		seen[s.WalletID] = true
		allocationCovered = allocationCovered && s.Covered
		class := s.Class
		if class == High {
			r.HasHighExposure = r.HasHighExposure || s.Amount.IsPositive()
		} else if !s.Covered || s.ScopeVersion != e.ScopeVersion || s.Revision <= 0 ||
			(class != Unknown && (s.ExpiresAt == nil || !now.Before(*s.ExpiresAt))) {
			class = Unknown
		}
		position := BalancePosition{Scope: e.Scope, Class: class}
		if s.Held || !s.Covered || s.ScopeVersion != e.ScopeVersion || s.Revision <= 0 {
			position.TechnicalHold = s.Amount
		} else {
			position.Free = s.Amount
		}
		positions = append(positions, position)
		attributed = attributed.Add(s.Amount)
	}
	// Existing debit-first requests do not yet carry exact net-source allocations.
	// Display their outstanding claim without guessing which class was reserved.
	switch {
	case !e.RoutingEnabled:
		r.Reason = "routing_not_enabled"
	case !e.LedgerMatches:
		r.Reason = "ledger_reconciliation_required"
	case unbackedExecution:
		r.Reason = "withdrawal_reconciliation_required"
	case r.Reserved.IsPositive() || r.PendingRestoration.IsPositive():
		r.Reason = "withdrawal_allocation_required"
	case attributed.Cmp(e.Ledger) > 0:
		r.Reason = "source_reconciliation_required"
	default:
		r.AllocationComplete = attributed.Equal(e.Ledger) && allocationCovered
		r.Reason = "source_allocation_complete"
		if !r.AllocationComplete {
			r.Reason = "source_allocation_incomplete"
		}
		positions = append(positions, BalancePosition{Scope: e.Scope, Class: Unknown, TechnicalHold: e.Ledger.Sub(attributed)})
	}
	if r.Reason != "source_allocation_complete" && r.Reason != "source_allocation_incomplete" {
		positions = []BalancePosition{{Scope: e.Scope, Class: Unknown, Reserved: r.Reserved, TechnicalHold: e.Ledger.Add(r.PendingRestoration)}}
	}
	var err error
	r.Buckets, err = SummarizeBalance(e.Scope, positions)
	if err != nil {
		return r, err
	}
	for _, b := range r.Buckets {
		r.Free = r.Free.Add(b.Free)
		r.TechnicalHold = r.TechnicalHold.Add(b.TechnicalHold)
	}
	return r, nil
}

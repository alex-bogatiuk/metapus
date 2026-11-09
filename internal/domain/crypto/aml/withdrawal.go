package aml

import (
	"bytes"
	"slices"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type Source struct {
	WalletID   id.ID
	MerchantID id.ID
	NetworkID  id.ID
	TokenID    id.ID
	Class      Class
	Revision   int64
	Available  types.CryptoAmount // net of fees, holds and existing reservations
}

type WithdrawalRequest struct {
	MerchantID   id.ID
	NetworkID    id.ID
	TokenID      id.ID
	Class        Class
	Amount       types.CryptoAmount
	RiskAccepted bool
}

func (r WithdrawalRequest) Validate() error {
	if id.IsNil(r.MerchantID) || id.IsNil(r.NetworkID) || id.IsNil(r.TokenID) || !r.Class.Valid() || !r.Amount.IsPositive() {
		return apperror.NewValidation("merchant, network, token, risk class and positive amount are required")
	}
	if (r.Class == High || r.Class == Unknown) && !r.RiskAccepted {
		return apperror.NewValidation("explicit acceptance is required for high or unknown risk withdrawal").WithDetail("field", "riskAccepted")
	}
	return nil
}

type SourceLine struct {
	WalletID id.ID
	Class    Class
	Revision int64
	Amount   types.CryptoAmount
}

// SelectSources is a deterministic plan, not a reservation. The repository
// must lock and revalidate these same revisions before atomically reserving.
func SelectSources(request WithdrawalRequest, candidates []Source) ([]SourceLine, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	ordered := slices.Clone(candidates)
	slices.SortFunc(ordered, func(a, b Source) int { return bytes.Compare(a.WalletID[:], b.WalletID[:]) })
	available := types.ZeroCryptoAmount()
	seen := make(map[id.ID]bool, len(ordered))
	for _, source := range ordered {
		if id.IsNil(source.WalletID) || source.Revision <= 0 || seen[source.WalletID] {
			return nil, apperror.NewValidation("invalid or duplicate withdrawal source")
		}
		seen[source.WalletID] = true
		if matchesSource(request, source) {
			available = available.Add(source.Available)
		}
	}
	if available.Cmp(request.Amount) < 0 {
		return nil, apperror.NewConflict("insufficient funds in selected risk class").
			WithDetail("riskClass", string(request.Class)).WithDetail("availableMinor", available.String())
	}
	remaining := request.Amount
	lines := make([]SourceLine, 0, len(ordered))
	for _, source := range ordered {
		if !matchesSource(request, source) || !source.Available.IsPositive() {
			continue
		}
		amount := source.Available
		if amount.Cmp(remaining) > 0 {
			amount = remaining
		}
		lines = append(lines, SourceLine{WalletID: source.WalletID, Class: request.Class, Revision: source.Revision, Amount: amount})
		remaining = remaining.Sub(amount)
		if remaining.IsZero() {
			break
		}
	}
	return lines, nil
}

func matchesSource(request WithdrawalRequest, source Source) bool {
	return source.MerchantID == request.MerchantID && source.NetworkID == request.NetworkID && source.TokenID == request.TokenID && source.Class == request.Class
}

// CanConsolidate checks automatic custody routing; explicit external exports
// have their own intent and never pass through this rule.
func CanConsolidate(source, destination Source) bool {
	return !id.IsNil(source.MerchantID) && !id.IsNil(source.NetworkID) && !id.IsNil(source.TokenID) &&
		!id.IsNil(source.WalletID) && !id.IsNil(destination.WalletID) && source.WalletID != destination.WalletID &&
		source.MerchantID == destination.MerchantID && source.NetworkID == destination.NetworkID && source.TokenID == destination.TokenID &&
		(source.Class == Low || source.Class == Medium) && source.Class == destination.Class
}

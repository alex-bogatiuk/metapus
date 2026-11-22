package aml

import (
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

type BalanceScope struct {
	MerchantID id.ID
	NetworkID  id.ID
	TokenID    id.ID
}

// BalancePosition partitions one net principal. Reserved and technically held
// amounts must not overlap; an in-flight request remains in Reserved.
type BalancePosition struct {
	Scope         BalanceScope
	Class         Class
	Free          types.RawAmount
	Reserved      types.RawAmount
	TechnicalHold types.RawAmount
}

type BalanceBucket struct {
	Class         Class           `json:"riskClass"`
	Total         types.RawAmount `json:"total"`
	Free          types.RawAmount `json:"free"`
	Reserved      types.RawAmount `json:"reserved"`
	TechnicalHold types.RawAmount `json:"technicalHold"`
}

// SummarizeBalance expects one merchant/network/token snapshot. It does not
// add the source reservations a second time to debit-first commitments.
func SummarizeBalance(scope BalanceScope, positions []BalancePosition) ([]BalanceBucket, error) {
	if id.IsNil(scope.MerchantID) || id.IsNil(scope.NetworkID) || id.IsNil(scope.TokenID) {
		return nil, apperror.NewValidation("merchant, network and token are required for risk balance")
	}
	classes := Classes()
	buckets := make([]BalanceBucket, len(classes))
	for i, class := range classes {
		buckets[i].Class = class
	}
	for _, position := range positions {
		if position.Scope != scope {
			return nil, apperror.NewValidation("risk balance cannot combine different merchants, networks or tokens")
		}
		if !position.Class.Valid() {
			return nil, apperror.NewValidation("invalid balance risk class")
		}
		for i := range buckets {
			if buckets[i].Class != position.Class {
				continue
			}
			buckets[i].Free = buckets[i].Free.Add(position.Free)
			buckets[i].Reserved = buckets[i].Reserved.Add(position.Reserved)
			buckets[i].TechnicalHold = buckets[i].TechnicalHold.Add(position.TechnicalHold)
			buckets[i].Total = buckets[i].Free.Add(buckets[i].Reserved).Add(buckets[i].TechnicalHold)
		}
	}
	return buckets, nil
}

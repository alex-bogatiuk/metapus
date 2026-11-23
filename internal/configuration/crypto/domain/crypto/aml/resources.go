package aml

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

// ResourceRequirement spends service operating capital only. A native funding
// amount is gas delivered to a wallet, never merchant principal or fee income.
type ResourceRequirement struct {
	Funding       types.RawAmount
	MaxFee        types.RawAmount
	RentalCovered bool
}

func (r ResourceRequirement) Total() types.RawAmount { return r.Funding.Add(r.MaxFee) }

func (r ResourceRequirement) ValidateAvailable(available types.RawAmount) error {
	if !r.MaxFee.IsPositive() {
		return apperror.NewValidation("resource fee cap must be positive")
	}
	required := r.Total()
	if r.RentalCovered {
		required = r.Funding
	}
	if required.Cmp(available) > 0 {
		return apperror.NewConflict("insufficient operating funds; merchant principal cannot fund gas").WithDetail("availableOperating", available.String())
	}
	return nil
}

type ResourceBudgetRequest struct {
	FeeReservationID   id.ID
	MerchantID         id.ID
	ResourceRevision   int64
	RiskRevision       int64
	ScopeVersion       int64
	Class              Class
	RiskExportAccepted bool `json:",omitempty"`
}

func (r ResourceBudgetRequest) Validate() error {
	if id.IsNil(r.FeeReservationID) || id.IsNil(r.MerchantID) || r.ResourceRevision <= 0 || r.RiskRevision <= 0 || r.ScopeVersion <= 0 || !r.Class.Valid() {
		return apperror.NewValidation("resource budget requires owner, current revisions and an explicit risk class")
	}
	if (r.Class == High || r.Class == Unknown) && !r.RiskExportAccepted {
		return apperror.NewValidation("high or unknown operating risk requires an explicitly accepted export")
	}
	return nil
}

type ResourceBudget struct {
	ID          id.ID
	Status      string
	Requirement ResourceRequirement
}

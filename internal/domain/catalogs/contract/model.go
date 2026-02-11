// Package contract provides the Contract catalog (Справочник "Договоры контрагентов").
// Contracts represent agreements between the organization and counterparties.
package contract

import (
	"context"
	"time"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// ContractType defines the type of contract.
type ContractType string

const (
	TypeSupply ContractType = "supply" // Договор поставки (с поставщиком)
	TypeSale   ContractType = "sale"   // Договор продажи (с покупателем)
	TypeOther  ContractType = "other"  // Прочие
)

// Contract represents a contract/agreement with a counterparty.
type Contract struct {
	entity.Catalog

	// CounterpartyID is the reference to the counterparty
	CounterpartyID id.ID `db:"counterparty_id" json:"counterpartyId"`

	// Type defines the contract type
	Type ContractType `db:"type" json:"type"`

	// CurrencyID is the optional currency for this contract
	CurrencyID *id.ID `db:"currency_id" json:"currencyId,omitempty"`

	// ValidFrom is the start date of the contract
	ValidFrom *time.Time `db:"valid_from" json:"validFrom,omitempty"`

	// ValidTo is the end date of the contract
	ValidTo *time.Time `db:"valid_to" json:"validTo,omitempty"`

	// PaymentTermDays is the payment term in days
	PaymentTermDays int `db:"payment_term_days" json:"paymentTermDays"`

	// Description is a detailed description
	Description *string `db:"description" json:"description,omitempty"`
}

// NewContract creates a new Contract with required fields.
func NewContract(code, name string, counterpartyID id.ID, contractType ContractType) *Contract {
	return &Contract{
		Catalog:        entity.NewCatalog(code, name),
		CounterpartyID: counterpartyID,
		Type:           contractType,
	}
}

// Validate implements entity.Validatable interface.
func (c *Contract) Validate(ctx context.Context) error {
	// Base catalog validation
	if err := c.Catalog.Validate(ctx); err != nil {
		return err
	}

	// CounterpartyID is required
	if id.IsNil(c.CounterpartyID) {
		return apperror.NewValidation("counterparty is required").
			WithDetail("field", "counterpartyId")
	}

	// Type validation
	if !isValidContractType(c.Type) {
		return apperror.NewValidation("invalid contract type").
			WithDetail("field", "type").
			WithDetail("value", string(c.Type))
	}

	// PaymentTermDays must be non-negative
	if c.PaymentTermDays < 0 {
		return apperror.NewValidation("payment term days must be non-negative").
			WithDetail("field", "paymentTermDays")
	}

	// ValidTo must be after ValidFrom
	if c.ValidFrom != nil && c.ValidTo != nil && c.ValidTo.Before(*c.ValidFrom) {
		return apperror.NewValidation("valid_to must be after valid_from").
			WithDetail("field", "validTo")
	}

	return nil
}

// IsActive checks if the contract is active at the given date.
func (c *Contract) IsActive(at time.Time) bool {
	if c.ValidFrom != nil && at.Before(*c.ValidFrom) {
		return false
	}
	if c.ValidTo != nil && at.After(*c.ValidTo) {
		return false
	}
	return true
}

// --- Validation Helpers ---

func isValidContractType(t ContractType) bool {
	switch t {
	case TypeSupply, TypeSale, TypeOther:
		return true
	}
	return false
}

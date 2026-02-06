package entity

import (
	"context"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// CurrencyAware is a trait for entities that have a currency dimension.
// Used for composition in models like GoodsReceipt, Invoice, etc.
type CurrencyAware struct {
	// CurrencyID is the primary currency for financial operations in this entity
	CurrencyID id.ID `db:"currency_id" json:"currencyId"`
}

// ValidateCurrency ensures a currency is set.
func (c *CurrencyAware) ValidateCurrency(ctx context.Context) error {
	if id.IsNil(c.CurrencyID) {
		return apperror.NewValidation("currency is required").
			WithDetail("field", "currencyId")
	}
	return nil
}

// GetCurrencyID returns the currency ID (useful for interfaces).
func (c *CurrencyAware) GetCurrencyID() id.ID {
	return c.CurrencyID
}

// ICurrencyAware is an interface for any document that has a currency.
type ICurrencyAware interface {
	GetCurrencyID() id.ID
	ValidateCurrency(ctx context.Context) error
}

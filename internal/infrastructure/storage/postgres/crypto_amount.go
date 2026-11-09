package postgres

import (
	"errors"

	"metapus/internal/core/types"
)

// NullableCryptoAmount is the PostgreSQL adapter for nullable NUMERIC(78,0)
// crypto columns. Domain value objects remain unaware of pgx nullable types.
type NullableCryptoAmount struct {
	Amount types.CryptoAmount
	Valid  bool
}

// Scan implements database/sql.Scanner through pgx's codec bridge.
func (n *NullableCryptoAmount) Scan(src any) error {
	if n == nil {
		return errors.New("cannot scan nullable crypto amount into nil receiver")
	}
	if src == nil {
		n.Amount = types.ZeroCryptoAmount()
		n.Valid = false
		return nil
	}
	n.Amount = types.ZeroCryptoAmount()
	n.Valid = false
	var amount types.CryptoAmount
	if err := amount.Scan(src); err != nil {
		return err
	}
	n.Amount = amount
	n.Valid = true
	return nil
}

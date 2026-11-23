package postgres

import (
	"errors"

	"github.com/alex-bogatiuk/metapus/internal/core/types"
)

// NullableRawAmount is the PostgreSQL adapter for nullable NUMERIC(78,0)
// raw amount columns. Domain value objects remain unaware of pgx nullable types.
type NullableRawAmount struct {
	Amount types.RawAmount
	Valid  bool
}

// Scan implements database/sql.Scanner through pgx's codec bridge.
func (n *NullableRawAmount) Scan(src any) error {
	if n == nil {
		return errors.New("cannot scan nullable raw amount into nil receiver")
	}
	if src == nil {
		n.Amount = types.ZeroRawAmount()
		n.Valid = false
		return nil
	}
	n.Amount = types.ZeroRawAmount()
	n.Valid = false
	var amount types.RawAmount
	if err := amount.Scan(src); err != nil {
		return err
	}
	n.Amount = amount
	n.Valid = true
	return nil
}

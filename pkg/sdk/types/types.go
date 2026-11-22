// Package types holds the platform's fixed-point values: quantities with four
// decimal places and money in a currency's minor units.
package types

import (
	"math/big"

	core "metapus/internal/core/types"
)

type (
	// Quantity is a quantity scaled by QuantityScale, stored as BIGINT.
	Quantity = core.Quantity
	// MinorUnits is an amount in the minor units of a document's currency.
	MinorUnits = core.MinorUnits
)

// QuantityScale is the scale of Quantity: 1 is 10000.
const QuantityScale = core.QuantityScale

// NewQuantityFromInt64Scaled returns the quantity v/QuantityScale.
func NewQuantityFromInt64Scaled(v int64) Quantity { return core.NewQuantityFromInt64Scaled(v) }

type (
	// PaletteColor names one colour of the shared interface palette. The value is a
	// key, not a colour code: the frontend draws each key with shades tuned for the
	// light and the dark theme. The empty value means "not chosen"; the interface
	// then picks a stable colour itself.
	PaletteColor = core.PaletteColor
	// RawAmount is an immutable, non-negative integer number of an asset's raw
	// units. Its zero value is valid and represents zero.
	//
	// The internal big.Int is never exposed or mutated. This is important because
	// math/big.Int values must not be shallow-copied after use. Keeping the pointer
	// private and cloning at every boundary gives RawAmount ordinary value
	// semantics while retaining arbitrary precision.
	//
	// JSON and database representations are canonical base-10 strings. A JSON
	// number is deliberately rejected because JavaScript cannot represent all
	// such integers exactly.
	RawAmount = core.RawAmount
	// RawDelta is an immutable signed difference between two RawAmount
	// values. It is used for reconciliation and incident evidence, never for
	// business amounts or ledger movement resources.
	RawDelta = core.RawDelta
)

// MustRawAmount parses a constant or panics. It is intended for constants
// and tests only.
func MustRawAmount(s string) core.RawAmount { return core.MustRawAmount(s) }

// NewRawAmountFromBigInt clones v and rejects negative values.
func NewRawAmountFromBigInt(v *big.Int) (core.RawAmount, error) {
	return core.NewRawAmountFromBigInt(v)
}

// NewRawAmountFromInt64 creates a value from a non-negative integer.
// It panics for a negative argument because negative raw amounts are a
// programming error. Use RawDelta for signed differences.
func NewRawAmountFromInt64(v int64) core.RawAmount { return core.NewRawAmountFromInt64(v) }

// NewRawAmountFromString parses a canonical non-negative base-10 integer.
// Accepted examples: "0", "1", "1000000000000000000".
// Whitespace, signs, leading zeroes, exponents and decimal fractions are
// rejected so the same value always has one representation in JSON and JWS.
func NewRawAmountFromString(s string) (core.RawAmount, error) { return core.NewRawAmountFromString(s) }

// ZeroRawAmount returns the additive identity.
func ZeroRawAmount() core.RawAmount { return core.ZeroRawAmount() }

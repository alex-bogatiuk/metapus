// Package types holds the platform's fixed-point values: quantities with four
// decimal places and money in a currency's minor units.
package types

import core "metapus/internal/core/types"

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

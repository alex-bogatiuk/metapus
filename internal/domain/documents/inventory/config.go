package inventory

import "metapus/internal/core/numerator"

const (
	// NumeratorStrategy defines the numbering strategy for this document type.
	// Inventory document uses Strict strategy to avoid gaps in sequential numbering.
	NumeratorStrategy = numerator.StrategyStrict
)

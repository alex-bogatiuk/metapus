package inventory

import "metapus/pkg/numerator"

const (
	// NumeratorStrategy defines the numbering strategy for this document type.
	// Inventory is an internal control document, so we could use Cached,
	// but Strict is safer for now.
	NumeratorStrategy = numerator.StrategyStrict
)

package crypto_invoice

import "metapus/internal/core/numerator"

const (
	// _numeratorStrategy defines the numbering strategy for crypto invoices.
	// Strict strategy ensures sequential numbering without gaps.
	_numeratorStrategy = numerator.StrategyStrict
)

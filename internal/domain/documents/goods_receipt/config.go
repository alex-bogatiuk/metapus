package goods_receipt

import "metapus/pkg/numerator"

const (
	// NumeratorStrategy defines the numbering strategy for this document type.
	// GoodsReceipt is a primary accounting document, so we use Strict strategy.
	NumeratorStrategy = numerator.StrategyStrict
)

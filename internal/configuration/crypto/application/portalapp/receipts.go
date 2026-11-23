package portalapp

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
	"github.com/shopspring/decimal"
)

// ReceiptChangePercent compares two periods of the same asset. Callers must not
// combine different tokens, networks or raw-unit scales before this operation.
func ReceiptChangePercent(current, previous types.RawAmount) string {
	if !previous.IsPositive() {
		if current.IsPositive() {
			return "+100.00"
		}
		return "0.00"
	}
	delta := decimal.NewFromBigInt(current.Difference(previous).BigInt(), 0)
	pct := delta.Mul(decimal.NewFromInt(100)).DivRound(decimal.NewFromBigInt(previous.BigInt(), 0), 2)
	if pct.IsPositive() {
		return "+" + pct.StringFixed(2)
	}
	return pct.StringFixed(2)
}

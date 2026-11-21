// Package tariff holds the processing fee rule shared by every document that
// credits a merchant: invoice payments and manual merchant credits.
package tariff

import "metapus/internal/core/types"

// Fee is a processing tariff in token minor units:
// clamp(Fixed + gross × PercentBP / 10000, Min, Max). A zero Min or Max means
// no floor or no cap.
type Fee struct {
	Fixed     types.RawAmount
	PercentBP int
	Min, Max  types.RawAmount
}

// Amount is the fee charged on a gross amount.
func (f Fee) Amount(gross types.RawAmount) types.RawAmount {
	total := f.Fixed.Add(gross.MulDiv(int64(f.PercentBP), 10000))
	if f.Min.IsPositive() && total.Cmp(f.Min) < 0 {
		total = f.Min
	}
	if f.Max.IsPositive() && total.Cmp(f.Max) > 0 {
		total = f.Max
	}
	return total
}

// Split divides a gross amount into the fee and what is credited. A fee equal
// to the gross credits nothing; a fee above it is rejected by the document
// that charges it, and Split then also credits nothing.
func (f Fee) Split(gross types.RawAmount) (fee, net types.RawAmount) {
	fee = f.Amount(gross)
	if fee.Cmp(gross) >= 0 {
		return fee, types.ZeroRawAmount()
	}
	return fee, gross.Sub(fee)
}

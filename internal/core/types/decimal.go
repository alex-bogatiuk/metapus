// Package types provides common type aliases and utilities.
package types

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

// Money represents a monetary value with full precision.
// Uses decimal.Decimal to avoid floating-point errors.
type Money = decimal.Decimal

// NewMoney creates a Money value from a float.
// WARNING: Use NewMoneyFromString for precise values.
func NewMoney(f float64) Money {
	return decimal.NewFromFloat(f)
}

// NewMoneyFromString creates a Money value from a string.
// This is the preferred method for monetary values.
func NewMoneyFromString(s string) (Money, error) {
	return decimal.NewFromString(s)
}

// MustMoney creates a Money value from a string, panics on error.
// Use only for constants.
func MustMoney(s string) Money {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// Zero returns zero Money value.
func Zero() Money {
	return decimal.Zero
}

// Quantity is a fixed-point quantity with 4 decimal places (scale = 1e4).
//
// Rationale:
// - Matches Postgres NUMERIC(15,4) semantics without floating point errors
// - Easy to store as BIGINT in DB (scaled integer)
// - JSON remains a number with up to 4 decimals
type Quantity int64

const QuantityScale int64 = 10_000

func NewQuantityFromFloat64(v float64) Quantity {
	q, err := CheckedQuantityFromFloat64(v)
	if err != nil {
		panic(err)
	}
	return q
}

func NewQuantityFromInt64Scaled(v int64) Quantity { return Quantity(v) }

func (q Quantity) Int64Scaled() int64 { return int64(q) }

func (q Quantity) Float64() float64 { return float64(q) / float64(QuantityScale) }

func (q Quantity) IsZero() bool { return q == 0 }

func (q Quantity) IsPositive() bool { return q > 0 }

func (q Quantity) IsNegative() bool { return q < 0 }

func (q Quantity) Neg() Quantity { return -q }

func (q Quantity) Abs() Quantity {
	if q < 0 {
		return -q
	}
	return q
}

// String returns a decimal string with 4 fractional digits.
func (q Quantity) String() string {
	negative := q < 0
	magnitude := uint64(q)
	if negative {
		magnitude = uint64(-(int64(q) + 1)) + 1
	}
	prefix := ""
	if negative {
		prefix = "-"
	}
	return fmt.Sprintf("%s%d.%04d", prefix, magnitude/uint64(QuantityScale), magnitude%uint64(QuantityScale))
}

// MarshalJSON encodes Quantity as JSON number (not string), preserving 4 digits.
func (q Quantity) MarshalJSON() ([]byte, error) {
	return []byte(q.String()), nil
}

// MarshalJSONTo implements json.MarshalerTo without an intermediate buffer.
func (q Quantity) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteValue(jsontext.Value(q.String()))
}

// UnmarshalJSON accepts either a JSON number or string and parses to fixed-point (4 digits).
func (q *Quantity) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return fmt.Errorf("quantity must be a decimal number or string")
	}

	// If string, unquote first.
	if len(data) >= 2 && data[0] == '"' && data[len(data)-1] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		parsed, err := parseQuantityString(s)
		if err != nil {
			return err
		}
		*q = parsed
		return nil
	}

	// Otherwise treat as number token.
	parsed, err := parseQuantityString(string(data))
	if err != nil {
		return err
	}
	*q = parsed
	return nil
}

// UnmarshalJSONFrom implements json.UnmarshalerFrom.
func (q *Quantity) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	return q.UnmarshalJSON(val)
}

// ParseQuantity accepts ordinary decimal notation, without whitespace,
// exponent notation, implicit rounding or overflow of the scaled result.
func ParseQuantity(s string) (Quantity, error) {
	if s == "" {
		return 0, fmt.Errorf("empty quantity")
	}
	negative := s[0] == '-'
	if negative {
		s = s[1:]
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || (hasDot && frac == "") || len(frac) > 4 {
		return 0, fmt.Errorf("quantity requires digits and at most four decimal places")
	}
	for _, part := range []string{whole, frac} {
		for i := range len(part) {
			if part[i] < '0' || part[i] > '9' {
				return 0, fmt.Errorf("invalid quantity digit")
			}
		}
	}
	scaled := whole + frac + strings.Repeat("0", 4-len(frac))
	if negative {
		scaled = "-" + scaled
	}
	value, err := strconv.ParseInt(scaled, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("quantity outside int64 scaled range: %w", err)
	}
	return Quantity(value), nil
}

func parseQuantityString(s string) (Quantity, error) { return ParseQuantity(s) }

// CheckedQuantityFromFloat64 explicitly rounds a calculated quantity to four
// places using the existing half-away-from-zero rule. User input uses ParseQuantity.
func CheckedQuantityFromFloat64(v float64) (Quantity, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("quantity must be finite")
	}
	return CheckedQuantityFromDecimal(decimal.NewFromFloat(v).Round(4))
}

func CheckedQuantityFromDecimal(v decimal.Decimal) (Quantity, error) {
	scaled, err := CheckedInt64FromDecimal(v.Shift(4))
	return Quantity(scaled), err
}

// CheckedInt64FromDecimal requires an integer and checks before conversion.
func CheckedInt64FromDecimal(v decimal.Decimal) (int64, error) {
	if !v.Equal(v.Truncate(0)) {
		return 0, fmt.Errorf("fractional integer result")
	}
	if v.LessThan(decimal.NewFromInt(math.MinInt64)) || v.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, fmt.Errorf("integer result outside int64 range")
	}
	return v.IntPart(), nil
}

func CheckedAddInt64(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, fmt.Errorf("integer sum outside int64 range")
	}
	return a + b, nil
}

// MinorUnits represents a monetary value in minor currency units (cents, kopecks, satoshi).
// Storage: int64 - sufficient for ±922 trillion minor units.
// Example: 123.45 RUB → 12345 (kopecks), 0.001 BTC → 100000 (satoshi)
type MinorUnits int64

// NewMinorUnitsFromDecimal creates MinorUnits from a decimal.Decimal major amount and decimal places.
// This is the preferred constructor — no floating-point precision loss.
func NewMinorUnitsFromDecimal(major decimal.Decimal, decimalPlaces int) MinorUnits {
	result, err := CheckedMinorUnitsFromDecimal(major, decimalPlaces)
	if err != nil {
		panic(err)
	}
	return result
}

// CheckedMinorUnitsFromDecimal rounds calculated money half away from zero,
// matching decimal.Round(0), and rejects invalid currency scale or overflow.
func CheckedMinorUnitsFromDecimal(major decimal.Decimal, decimalPlaces int) (MinorUnits, error) {
	if decimalPlaces < 0 || decimalPlaces > 18 {
		return 0, fmt.Errorf("currency scale must be between 0 and 18")
	}
	value, err := CheckedInt64FromDecimal(major.Shift(int32(decimalPlaces)).Round(0))
	return MinorUnits(value), err
}

// ToDecimal converts minor units back to major units as decimal.Decimal.
func (m MinorUnits) ToDecimal(decimalPlaces int) decimal.Decimal {
	return decimal.NewFromInt(int64(m)).Shift(-int32(decimalPlaces))
}

func (m MinorUnits) IsZero() bool     { return m == 0 }
func (m MinorUnits) IsPositive() bool { return m > 0 }
func (m MinorUnits) IsNegative() bool { return m < 0 }
func (m MinorUnits) Neg() MinorUnits  { return -m }
func (m MinorUnits) Abs() MinorUnits {
	if m < 0 {
		return -m
	}
	return m
}

// MarshalJSON encodes MinorUnits as a JSON number.
func (m MinorUnits) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(m), 10)), nil
}

// MarshalJSONTo implements json.MarshalerTo without an intermediate buffer.
func (m MinorUnits) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.Int(int64(m)))
}

// UnmarshalJSON decodes MinorUnits from a JSON number or string.
func (m *MinorUnits) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*m = 0
		return nil
	}

	// Strip quotes if string
	s := string(data)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return fmt.Errorf("parse MinorUnits: %w", err)
	}
	*m = MinorUnits(v)
	return nil
}

// UnmarshalJSONFrom implements json.UnmarshalerFrom.
func (m *MinorUnits) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	val, err := dec.ReadValue()
	if err != nil {
		return err
	}
	return m.UnmarshalJSON(val)
}

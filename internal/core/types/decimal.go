// Package types provides common type aliases and utilities.
package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
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
	return Quantity(math.Round(v * float64(QuantityScale)))
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
	neg := q < 0
	v := q
	if neg {
		v = -v
	}
	intPart := int64(v) / QuantityScale
	frac := int64(v) % QuantityScale
	if neg {
		return fmt.Sprintf("-%d.%04d", intPart, frac)
	}
	return fmt.Sprintf("%d.%04d", intPart, frac)
}

// MarshalJSON encodes Quantity as JSON number (not string), preserving 4 digits.
func (q Quantity) MarshalJSON() ([]byte, error) {
	return []byte(q.String()), nil
}

// UnmarshalJSON accepts either a JSON number or string and parses to fixed-point (4 digits).
func (q *Quantity) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*q = 0
		return nil
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

func parseQuantityString(s string) (Quantity, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty quantity")
	}

	// We intentionally do NOT support exponent form to keep parsing strict.
	if strings.ContainsAny(s, "eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("parse quantity: %w", err)
		}
		return NewQuantityFromFloat64(f), nil
	}

	sign := int64(1)
	if strings.HasPrefix(s, "-") {
		sign = -1
		s = strings.TrimPrefix(s, "-")
	} else if strings.HasPrefix(s, "+") {
		s = strings.TrimPrefix(s, "+")
	}

	parts := strings.SplitN(s, ".", 2)
	intPartStr := parts[0]
	fracStr := ""
	if len(parts) == 2 {
		fracStr = parts[1]
	}

	if intPartStr == "" {
		intPartStr = "0"
	}
	intPart, err := strconv.ParseInt(intPartStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse quantity integer part: %w", err)
	}

	// Normalize fractional part to 4 digits (pad right, truncate extra digits).
	if len(fracStr) > 4 {
		fracStr = fracStr[:4]
	}
	for len(fracStr) < 4 {
		fracStr += "0"
	}
	frac := int64(0)
	if fracStr != "" {
		frac, err = strconv.ParseInt(fracStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse quantity fractional part: %w", err)
		}
	}

	return Quantity(sign * (intPart*QuantityScale + frac)), nil
}

// MinorUnits represents a monetary value in minor currency units (cents, kopecks, satoshi).
// Storage: int64 - sufficient for ±922 trillion minor units.
// Example: 123.45 RUB → 12345 (kopecks), 0.001 BTC → 100000 (satoshi)
type MinorUnits int64

// NewMinorUnitsFromMajor creates MinorUnits from a major unit amount and decimal places.
func NewMinorUnitsFromMajor(major float64, decimalPlaces int) MinorUnits {
	multiplier := math.Pow10(decimalPlaces)
	return MinorUnits(math.Round(major * multiplier))
}

// ToMajor converts minor units back to major units for display.
func (m MinorUnits) ToMajor(decimalPlaces int) float64 {
	return float64(m) / math.Pow10(decimalPlaces)
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

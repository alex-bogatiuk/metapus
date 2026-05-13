// Package types provides common type aliases and utilities.
//
// CryptoAmount — int64 type for cryptocurrency minor units.
// Covers all supported chains: TRON/USDT (6 dec), BTC (8 dec), ETH in gwei (9 dec),
// SOL (9 dec), TON (9 dec). int64 max ≈ 9.2 × 10¹⁸.
//
// For ETH native: store in gwei (10⁹ wei), NOT wei. Token.DecimalPlaces = 9.
package types

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"

	"github.com/shopspring/decimal"
)

// CryptoAmount represents a cryptocurrency value in minor units (satoshi, sun, gwei, lamport).
// Uses int64 — sufficient for all chains when ETH uses gwei (not wei).
//
// Storage: Postgres BIGINT (8 bytes, indexed natively).
// JSON:    number (int64 values are always within JS safe integer range for practical amounts).
// Go:      value type, zero allocs.
type CryptoAmount int64

// ZeroCryptoAmount returns a zero CryptoAmount.
func ZeroCryptoAmount() CryptoAmount {
	return 0
}

// NewCryptoAmountFromInt64 creates a CryptoAmount from an int64.
func NewCryptoAmountFromInt64(v int64) CryptoAmount {
	return CryptoAmount(v)
}

// NewCryptoAmountFromString creates a CryptoAmount from a decimal string of minor units.
// Returns error if the string is not a valid integer or overflows int64.
func NewCryptoAmountFromString(s string) (CryptoAmount, error) {
	if s == "" || s == "null" {
		return 0, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CryptoAmount: %q: %w", s, err)
	}
	return CryptoAmount(v), nil
}

// MustCryptoAmount creates a CryptoAmount from a string, panics on error.
// Use only for constants and tests.
func MustCryptoAmount(s string) CryptoAmount {
	a, err := NewCryptoAmountFromString(s)
	if err != nil {
		panic(err)
	}
	return a
}

// --- Arithmetic (with overflow detection) ---

// Add returns a + b. Panics on overflow (financial invariant).
func (a CryptoAmount) Add(b CryptoAmount) CryptoAmount {
	result := int64(a) + int64(b)
	// Overflow: same-sign operands produce different-sign result
	if (int64(b) > 0 && result < int64(a)) || (int64(b) < 0 && result > int64(a)) {
		panic(fmt.Sprintf("CryptoAmount overflow: %d + %d", a, b))
	}
	return CryptoAmount(result)
}

// Sub returns a - b. Panics on overflow (financial invariant).
func (a CryptoAmount) Sub(b CryptoAmount) CryptoAmount {
	result := int64(a) - int64(b)
	// Overflow: subtraction overflow check
	if (int64(b) > 0 && result > int64(a)) || (int64(b) < 0 && result < int64(a)) {
		panic(fmt.Sprintf("CryptoAmount overflow: %d - %d", a, b))
	}
	return CryptoAmount(result)
}

// Neg returns -a. Panics on overflow (math.MinInt64).
func (a CryptoAmount) Neg() CryptoAmount {
	if int64(a) == math.MinInt64 {
		panic("CryptoAmount overflow: cannot negate MinInt64")
	}
	return CryptoAmount(-int64(a))
}

// Abs returns |a|. Panics on overflow (math.MinInt64).
func (a CryptoAmount) Abs() CryptoAmount {
	if int64(a) < 0 {
		return a.Neg()
	}
	return a
}

// MulDiv computes a * numerator / denominator using integer arithmetic.
// Floor division (rounds toward zero) — in commission calculation this
// favors the merchant (fee rounds down).
// Panics if denominator == 0 or on overflow.
func (a CryptoAmount) MulDiv(numerator, denominator int64) CryptoAmount {
	if denominator == 0 {
		panic("CryptoAmount.MulDiv: division by zero")
	}
	product, ok := safeMultiply(int64(a), numerator)
	if !ok {
		panic(fmt.Sprintf("CryptoAmount overflow: %d * %d", a, numerator))
	}
	return CryptoAmount(product / denominator)
}

// safeMultiply returns a * b and true if the result fits int64.
// Returns (0, false) on overflow.
func safeMultiply(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	product := a * b
	if product/a != b {
		return 0, false
	}
	return product, true
}

// --- Comparison ---

// Cmp compares a and b: -1 if a < b, 0 if a == b, +1 if a > b.
func (a CryptoAmount) Cmp(b CryptoAmount) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// IsZero returns true if a == 0.
func (a CryptoAmount) IsZero() bool {
	return a == 0
}

// IsPositive returns true if a > 0.
func (a CryptoAmount) IsPositive() bool {
	return a > 0
}

// IsNegative returns true if a < 0.
func (a CryptoAmount) IsNegative() bool {
	return a < 0
}

// --- Conversion ---

// Int64 returns the underlying int64 value.
func (a CryptoAmount) Int64() int64 {
	return int64(a)
}

// ToDecimal converts minor units to major units as decimal.Decimal.
// Example: CryptoAmount(1_000_000).ToDecimal(6) → 1.000000 (1 USDT).
func (a CryptoAmount) ToDecimal(decimalPlaces int) decimal.Decimal {
	return decimal.New(int64(a), -int32(decimalPlaces))
}

// String returns the decimal string representation.
func (a CryptoAmount) String() string {
	return strconv.FormatInt(int64(a), 10)
}

// --- JSON: encode as number (int64 values are safe for JS) ---

// MarshalJSON encodes CryptoAmount as a JSON number.
func (a CryptoAmount) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, int64(a), 10), nil
}

// UnmarshalJSON decodes CryptoAmount from a JSON number or string.
func (a *CryptoAmount) UnmarshalJSON(data []byte) error {
	s := string(data)
	// Strip quotes if present (backward compat with old string encoding)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "null" || s == "" {
		*a = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("CryptoAmount: invalid value %q: %w", s, err)
	}
	*a = CryptoAmount(v)
	return nil
}

// --- SQL: Postgres BIGINT ↔ int64 ---

// Value implements driver.Valuer for Postgres BIGINT.
func (a CryptoAmount) Value() (driver.Value, error) {
	return int64(a), nil
}

// Scan implements sql.Scanner for Postgres BIGINT.
func (a *CryptoAmount) Scan(src any) error {
	if src == nil {
		*a = 0
		return nil
	}

	switch v := src.(type) {
	case int64:
		*a = CryptoAmount(v)
		return nil
	case float64:
		*a = CryptoAmount(int64(v))
		return nil
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("CryptoAmount.Scan: invalid string %q: %w", v, err)
		}
		*a = CryptoAmount(parsed)
		return nil
	case []byte:
		parsed, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return fmt.Errorf("CryptoAmount.Scan: invalid bytes %q: %w", v, err)
		}
		*a = CryptoAmount(parsed)
		return nil
	default:
		return fmt.Errorf("CryptoAmount.Scan: unsupported type %T", src)
	}
}

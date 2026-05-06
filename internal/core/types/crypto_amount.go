// Package types provides common type aliases and utilities.
//
// CryptoAmount — arbitrary-precision integer for cryptocurrency minor units.
// int64 (MinorUnits) overflows at ~9.2 ETH in wei (18 decimals).
// CryptoAmount uses math/big.Int internally and maps to NUMERIC in Postgres.
package types

import (
	"database/sql/driver"
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"
)

// CryptoAmount represents a cryptocurrency value in minor units (satoshi, wei, sun, lamport).
// Uses math/big.Int for arbitrary precision — int64 is NOT sufficient for 18-decimal tokens.
//
// Storage: Postgres NUMERIC (arbitrary precision).
// JSON:    string (to avoid JS float64 precision loss for large integers).
// Go:      immutable value type — all operations return new values.
type CryptoAmount struct {
	val *big.Int
}

// _zeroBig is a shared zero value; never mutated.
var _zeroBig = new(big.Int)

// ZeroCryptoAmount returns a zero CryptoAmount.
func ZeroCryptoAmount() CryptoAmount {
	return CryptoAmount{val: new(big.Int)}
}

// NewCryptoAmount creates a CryptoAmount from a big.Int (defensive copy).
func NewCryptoAmount(v *big.Int) CryptoAmount {
	if v == nil {
		return CryptoAmount{val: new(big.Int)}
	}
	return CryptoAmount{val: new(big.Int).Set(v)}
}

// NewCryptoAmountFromInt64 creates a CryptoAmount from an int64.
func NewCryptoAmountFromInt64(v int64) CryptoAmount {
	return CryptoAmount{val: big.NewInt(v)}
}

// NewCryptoAmountFromString creates a CryptoAmount from a decimal string of minor units.
// Returns error if the string is not a valid integer.
func NewCryptoAmountFromString(s string) (CryptoAmount, error) {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return CryptoAmount{}, fmt.Errorf("invalid CryptoAmount: %q", s)
	}
	return CryptoAmount{val: v}, nil
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

// bigVal returns the underlying big.Int (never nil).
func (a CryptoAmount) bigVal() *big.Int {
	if a.val == nil {
		return _zeroBig
	}
	return a.val
}

// --- Arithmetic (all return new values — immutable) ---

// Add returns a + b.
func (a CryptoAmount) Add(b CryptoAmount) CryptoAmount {
	return CryptoAmount{val: new(big.Int).Add(a.bigVal(), b.bigVal())}
}

// Sub returns a - b.
func (a CryptoAmount) Sub(b CryptoAmount) CryptoAmount {
	return CryptoAmount{val: new(big.Int).Sub(a.bigVal(), b.bigVal())}
}

// Neg returns -a.
func (a CryptoAmount) Neg() CryptoAmount {
	return CryptoAmount{val: new(big.Int).Neg(a.bigVal())}
}

// Abs returns |a|.
func (a CryptoAmount) Abs() CryptoAmount {
	return CryptoAmount{val: new(big.Int).Abs(a.bigVal())}
}

// --- Comparison ---

// Cmp compares a and b: -1 if a < b, 0 if a == b, +1 if a > b.
func (a CryptoAmount) Cmp(b CryptoAmount) int {
	return a.bigVal().Cmp(b.bigVal())
}

// IsZero returns true if a == 0.
func (a CryptoAmount) IsZero() bool {
	return a.bigVal().Sign() == 0
}

// IsPositive returns true if a > 0.
func (a CryptoAmount) IsPositive() bool {
	return a.bigVal().Sign() > 0
}

// IsNegative returns true if a < 0.
func (a CryptoAmount) IsNegative() bool {
	return a.bigVal().Sign() < 0
}

// --- Conversion ---

// BigInt returns a defensive copy of the underlying big.Int.
func (a CryptoAmount) BigInt() *big.Int {
	return new(big.Int).Set(a.bigVal())
}

// ToDecimal converts minor units to major units as decimal.Decimal.
// Example: CryptoAmount(1_000_000).ToDecimal(6) → 1.000000 (1 USDT).
func (a CryptoAmount) ToDecimal(decimalPlaces int) decimal.Decimal {
	return decimal.NewFromBigInt(a.bigVal(), -int32(decimalPlaces))
}

// String returns the decimal string representation.
func (a CryptoAmount) String() string {
	return a.bigVal().String()
}

// --- JSON: encode as string to prevent JS float64 precision loss ---

// MarshalJSON encodes CryptoAmount as a JSON string.
// String encoding prevents precision loss in JavaScript (max safe int = 2^53).
func (a CryptoAmount) MarshalJSON() ([]byte, error) {
	return []byte(`"` + a.bigVal().String() + `"`), nil
}

// UnmarshalJSON decodes CryptoAmount from a JSON string or number.
func (a *CryptoAmount) UnmarshalJSON(data []byte) error {
	s := string(data)
	// Strip quotes if present
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "null" || s == "" {
		a.val = new(big.Int)
		return nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return fmt.Errorf("CryptoAmount: invalid value %q", s)
	}
	a.val = v
	return nil
}

// --- SQL: Postgres NUMERIC ↔ big.Int ---

// Value implements driver.Valuer for Postgres NUMERIC.
func (a CryptoAmount) Value() (driver.Value, error) {
	return a.bigVal().String(), nil
}

// Scan implements sql.Scanner for Postgres NUMERIC.
func (a *CryptoAmount) Scan(src any) error {
	if src == nil {
		a.val = new(big.Int)
		return nil
	}

	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	case int64:
		a.val = big.NewInt(v)
		return nil
	case float64:
		// NUMERIC may arrive as float64 from some drivers.
		// Convert via decimal to avoid precision loss.
		d := decimal.NewFromFloat(v)
		a.val = d.BigInt()
		return nil
	default:
		return fmt.Errorf("CryptoAmount.Scan: unsupported type %T", src)
	}

	val, ok := new(big.Int).SetString(s, 10)
	if !ok {
		// Try parsing as decimal string (e.g., "1000000.00" from NUMERIC)
		d, err := decimal.NewFromString(s)
		if err != nil {
			return fmt.Errorf("CryptoAmount.Scan: invalid value %q", s)
		}
		a.val = d.BigInt()
		return nil
	}
	a.val = val
	return nil
}

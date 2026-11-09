// Package types contains precision-safe value objects shared by the crypto
// domain. Raw blockchain values are integers; their decimal scale belongs to
// the asset metadata and is never embedded in CryptoAmount.
package types

import (
	"database/sql/driver"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/shopspring/decimal"

	"metapus/internal/core/json"
)

var errNegativeCryptoAmount = errors.New("crypto amount cannot be negative")

// CryptoAmount is an immutable, non-negative integer number of blockchain raw
// units. Its zero value is valid and represents zero.
//
// The internal big.Int is never exposed or mutated. This is important because
// math/big.Int values must not be shallow-copied after use. Keeping the pointer
// private and cloning at every boundary gives CryptoAmount ordinary value
// semantics while retaining arbitrary precision.
//
// JSON and database representations are canonical base-10 strings. A JSON
// number is deliberately rejected because JavaScript cannot represent all
// blockchain integers exactly.
type CryptoAmount struct {
	value *big.Int
	_     [0]func() // make accidental == comparisons a compile-time error
}

// ZeroCryptoAmount returns the additive identity.
func ZeroCryptoAmount() CryptoAmount { return CryptoAmount{} }

// NewCryptoAmountFromInt64 creates a value from a non-negative integer.
// It panics for a negative argument because negative raw amounts are a
// programming error. Use CryptoDelta for signed differences.
func NewCryptoAmountFromInt64(v int64) CryptoAmount {
	if v < 0 {
		panic(errNegativeCryptoAmount)
	}
	if v == 0 {
		return ZeroCryptoAmount()
	}
	return CryptoAmount{value: big.NewInt(v)}
}

// NewCryptoAmountFromUint64 creates a value from an unsigned integer.
func NewCryptoAmountFromUint64(v uint64) CryptoAmount {
	if v == 0 {
		return ZeroCryptoAmount()
	}
	return CryptoAmount{value: new(big.Int).SetUint64(v)}
}

// NewCryptoAmountFromBigInt clones v and rejects negative values.
func NewCryptoAmountFromBigInt(v *big.Int) (CryptoAmount, error) {
	if v == nil || v.Sign() == 0 {
		return ZeroCryptoAmount(), nil
	}
	if v.Sign() < 0 {
		return ZeroCryptoAmount(), errNegativeCryptoAmount
	}
	return CryptoAmount{value: new(big.Int).Set(v)}, nil
}

// NewCryptoAmountFromString parses a canonical non-negative base-10 integer.
// Accepted examples: "0", "1", "1000000000000000000".
// Whitespace, signs, leading zeroes, exponents and decimal fractions are
// rejected so the same value always has one representation in JSON and JWS.
func NewCryptoAmountFromString(s string) (CryptoAmount, error) {
	if !isCanonicalUnsignedInteger(s) {
		return ZeroCryptoAmount(), fmt.Errorf("invalid crypto amount %q: expected canonical non-negative integer", s)
	}
	if s == "0" {
		return ZeroCryptoAmount(), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return ZeroCryptoAmount(), fmt.Errorf("invalid crypto amount %q", s)
	}
	return CryptoAmount{value: v}, nil
}

// MustCryptoAmount parses a constant or panics. It is intended for constants
// and tests only.
func MustCryptoAmount(s string) CryptoAmount {
	a, err := NewCryptoAmountFromString(s)
	if err != nil {
		panic(err)
	}
	return a
}

// Add returns a+b without changing either operand.
func (a CryptoAmount) Add(b CryptoAmount) CryptoAmount {
	return cryptoAmountFromNonNegative(new(big.Int).Add(a.bigInt(), b.bigInt()))
}

// Sub returns a-b without changing either operand. It panics on underflow;
// callers that need a signed result must use Difference.
func (a CryptoAmount) Sub(b CryptoAmount) CryptoAmount {
	result := new(big.Int).Sub(a.bigInt(), b.bigInt())
	if result.Sign() < 0 {
		panic(fmt.Sprintf("crypto amount underflow: %s - %s", a, b))
	}
	return cryptoAmountFromNonNegative(result)
}

// Difference returns the signed difference a-b.
func (a CryptoAmount) Difference(b CryptoAmount) CryptoDelta {
	return cryptoDeltaFromBigInt(new(big.Int).Sub(a.bigInt(), b.bigInt()))
}

// MulDiv computes floor(a*numerator/denominator) using arbitrary-precision
// integer arithmetic. Negative factors and a non-positive denominator are
// invalid for a non-negative raw amount and panic as programming errors.
func (a CryptoAmount) MulDiv(numerator, denominator int64) CryptoAmount {
	if numerator < 0 {
		panic("CryptoAmount.MulDiv: numerator cannot be negative")
	}
	if denominator <= 0 {
		panic("CryptoAmount.MulDiv: denominator must be positive")
	}
	product := new(big.Int).Mul(a.bigInt(), big.NewInt(numerator))
	return cryptoAmountFromNonNegative(product.Quo(product, big.NewInt(denominator)))
}

// Cmp compares a and b and returns -1, 0, or +1.
func (a CryptoAmount) Cmp(b CryptoAmount) int { return a.bigInt().Cmp(b.bigInt()) }

// Equal reports whether a and b contain the same number of raw units.
func (a CryptoAmount) Equal(b CryptoAmount) bool { return a.Cmp(b) == 0 }

// IsZero reports whether a is zero.
func (a CryptoAmount) IsZero() bool { return a.value == nil || a.value.Sign() == 0 }

// IsPositive reports whether a is greater than zero.
func (a CryptoAmount) IsPositive() bool { return a.value != nil && a.value.Sign() > 0 }

// BigInt returns an independent copy of the raw integer.
func (a CryptoAmount) BigInt() *big.Int { return a.bigInt() }

// Int64Exact converts a to int64 and returns an error rather than truncating.
// It is intended only for chain adapters whose external protocol is bounded to
// int64 (for example, the current TRON HTTP API).
func (a CryptoAmount) Int64Exact() (int64, error) {
	v := a.bigInt()
	if !v.IsInt64() {
		return 0, fmt.Errorf("crypto amount %s does not fit int64", a)
	}
	return v.Int64(), nil
}

// ToDecimal applies asset metadata scale for display or conversion. The raw
// amount remains an integer.
func (a CryptoAmount) ToDecimal(decimalPlaces int) decimal.Decimal {
	return decimal.NewFromBigInt(a.bigInt(), -int32(decimalPlaces))
}

func (a CryptoAmount) String() string {
	if a.IsZero() {
		return "0"
	}
	return a.value.String()
}

// MarshalText implements encoding.TextMarshaler.
func (a CryptoAmount) MarshalText() ([]byte, error) { return []byte(a.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (a *CryptoAmount) UnmarshalText(text []byte) error {
	if a == nil {
		return errors.New("cannot unmarshal crypto amount into nil receiver")
	}
	parsed, err := NewCryptoAmountFromString(string(text))
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// MarshalJSON always emits a quoted canonical decimal string.
func (a CryptoAmount) MarshalJSON() ([]byte, error) { return json.Marshal(a.String()) }

// MarshalJSONTo implements json.MarshalerTo without an intermediate buffer.
func (a CryptoAmount) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(a.String()))
}

// UnmarshalJSON accepts only a quoted canonical decimal string.
func (a *CryptoAmount) UnmarshalJSON(data []byte) error {
	if a == nil {
		return errors.New("cannot unmarshal crypto amount into nil receiver")
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err != nil {
		return fmt.Errorf("crypto amount must be a quoted decimal string: %w", err)
	}
	return a.UnmarshalText([]byte(encoded))
}

// UnmarshalJSONFrom implements json.UnmarshalerFrom.
func (a *CryptoAmount) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if a == nil {
		return errors.New("cannot unmarshal crypto amount into nil receiver")
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '"' {
		return fmt.Errorf("crypto amount must be a quoted decimal string")
	}
	return a.UnmarshalText([]byte(tok.String()))
}

// Value implements database/sql/driver.Valuer. PostgreSQL casts the canonical
// string to NUMERIC(78,0), avoiding a dependency from core types to pgx.
func (a CryptoAmount) Value() (driver.Value, error) { return a.String(), nil }

// Scan implements database/sql.Scanner for exact NUMERIC values. NULL and
// fractional values are rejected; nullable columns use the PostgreSQL adapter
// NullableCryptoAmount so NULL cannot be confused with zero.
func (a *CryptoAmount) Scan(src any) error {
	if a == nil {
		return errors.New("cannot scan crypto amount into nil receiver")
	}
	if src == nil {
		return errors.New("cannot scan NULL into CryptoAmount")
	}

	var encoded string
	switch v := src.(type) {
	case int64:
		if v < 0 {
			return errNegativeCryptoAmount
		}
		*a = NewCryptoAmountFromInt64(v)
		return nil
	case string:
		encoded = v
	case []byte:
		encoded = string(v)
	default:
		return fmt.Errorf("cannot scan CryptoAmount from %T", src)
	}
	return a.UnmarshalText([]byte(encoded))
}

func (a CryptoAmount) bigInt() *big.Int {
	if a.value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(a.value)
}

func cryptoAmountFromNonNegative(v *big.Int) CryptoAmount {
	if v == nil || v.Sign() == 0 {
		return ZeroCryptoAmount()
	}
	if v.Sign() < 0 {
		panic(errNegativeCryptoAmount)
	}
	return CryptoAmount{value: new(big.Int).Set(v)}
}

func isCanonicalUnsignedInteger(s string) bool {
	if s == "0" {
		return true
	}
	if len(s) == 0 || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// CryptoDelta is an immutable signed difference between two CryptoAmount
// values. It is used for reconciliation and incident evidence, never for
// business amounts or ledger movement resources.
type CryptoDelta struct {
	value *big.Int
	_     [0]func()
}

func ZeroCryptoDelta() CryptoDelta { return CryptoDelta{} }

func NewCryptoDeltaFromString(s string) (CryptoDelta, error) {
	if !isCanonicalSignedInteger(s) {
		return ZeroCryptoDelta(), fmt.Errorf("invalid crypto delta %q: expected canonical integer", s)
	}
	if s == "0" {
		return ZeroCryptoDelta(), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return ZeroCryptoDelta(), fmt.Errorf("invalid crypto delta %q", s)
	}
	return cryptoDeltaFromBigInt(v), nil
}

func (d CryptoDelta) Add(other CryptoDelta) CryptoDelta {
	return cryptoDeltaFromBigInt(new(big.Int).Add(d.bigInt(), other.bigInt()))
}

func (d CryptoDelta) Neg() CryptoDelta {
	return cryptoDeltaFromBigInt(new(big.Int).Neg(d.bigInt()))
}

func (d CryptoDelta) Abs() CryptoAmount {
	return cryptoAmountFromNonNegative(new(big.Int).Abs(d.bigInt()))
}

func (d CryptoDelta) Cmp(other CryptoDelta) int { return d.bigInt().Cmp(other.bigInt()) }
func (d CryptoDelta) IsZero() bool              { return d.value == nil || d.value.Sign() == 0 }
func (d CryptoDelta) IsPositive() bool          { return d.value != nil && d.value.Sign() > 0 }
func (d CryptoDelta) IsNegative() bool          { return d.value != nil && d.value.Sign() < 0 }
func (d CryptoDelta) BigInt() *big.Int          { return d.bigInt() }

func (d CryptoDelta) String() string {
	if d.IsZero() {
		return "0"
	}
	return d.value.String()
}

func (d CryptoDelta) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *CryptoDelta) UnmarshalText(text []byte) error {
	if d == nil {
		return errors.New("cannot unmarshal crypto delta into nil receiver")
	}
	parsed, err := NewCryptoDeltaFromString(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d CryptoDelta) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d CryptoDelta) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(d.String()))
}

func (d *CryptoDelta) UnmarshalJSON(data []byte) error {
	if d == nil {
		return errors.New("cannot unmarshal crypto delta into nil receiver")
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err != nil {
		return fmt.Errorf("crypto delta must be a quoted decimal string: %w", err)
	}
	return d.UnmarshalText([]byte(encoded))
}

func (d *CryptoDelta) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if d == nil {
		return errors.New("cannot unmarshal crypto delta into nil receiver")
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '"' {
		return fmt.Errorf("crypto delta must be a quoted decimal string")
	}
	return d.UnmarshalText([]byte(tok.String()))
}

func (d CryptoDelta) Value() (driver.Value, error) { return d.String(), nil }

func (d *CryptoDelta) Scan(src any) error {
	if d == nil {
		return errors.New("cannot scan crypto delta into nil receiver")
	}
	if src == nil {
		return errors.New("cannot scan NULL into CryptoDelta")
	}
	var encoded string
	switch v := src.(type) {
	case int64:
		encoded = strconv.FormatInt(v, 10)
	case string:
		encoded = v
	case []byte:
		encoded = string(v)
	default:
		return fmt.Errorf("cannot scan CryptoDelta from %T", src)
	}
	return d.UnmarshalText([]byte(encoded))
}

func (d CryptoDelta) bigInt() *big.Int {
	if d.value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(d.value)
}

func cryptoDeltaFromBigInt(v *big.Int) CryptoDelta {
	if v == nil || v.Sign() == 0 {
		return ZeroCryptoDelta()
	}
	return CryptoDelta{value: new(big.Int).Set(v)}
}

func isCanonicalSignedInteger(s string) bool {
	if s == "0" {
		return true
	}
	if len(s) > 1 && s[0] == '-' {
		return isCanonicalUnsignedInteger(s[1:]) && s[1:] != "0"
	}
	return isCanonicalUnsignedInteger(s)
}

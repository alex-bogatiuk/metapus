// Package types contains precision-safe value objects. A RawAmount counts an
// asset's smallest units (for example a token's base units) as an integer of
// arbitrary precision; its decimal scale belongs to the asset metadata and is
// never embedded in RawAmount.
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

var errNegativeRawAmount = errors.New("raw amount cannot be negative")

// RawAmount is an immutable, non-negative integer number of an asset's raw
// units. Its zero value is valid and represents zero.
//
// The internal big.Int is never exposed or mutated. This is important because
// math/big.Int values must not be shallow-copied after use. Keeping the pointer
// private and cloning at every boundary gives RawAmount ordinary value
// semantics while retaining arbitrary precision.
//
// JSON and database representations are canonical base-10 strings. A JSON
// number is deliberately rejected because JavaScript cannot represent all
// such integers exactly.
type RawAmount struct {
	value *big.Int
	_     [0]func() // make accidental == comparisons a compile-time error
}

// ZeroRawAmount returns the additive identity.
func ZeroRawAmount() RawAmount { return RawAmount{} }

// NewRawAmountFromInt64 creates a value from a non-negative integer.
// It panics for a negative argument because negative raw amounts are a
// programming error. Use RawDelta for signed differences.
func NewRawAmountFromInt64(v int64) RawAmount {
	if v < 0 {
		panic(errNegativeRawAmount)
	}
	if v == 0 {
		return ZeroRawAmount()
	}
	return RawAmount{value: big.NewInt(v)}
}

// NewRawAmountFromUint64 creates a value from an unsigned integer.
func NewRawAmountFromUint64(v uint64) RawAmount {
	if v == 0 {
		return ZeroRawAmount()
	}
	return RawAmount{value: new(big.Int).SetUint64(v)}
}

// NewRawAmountFromBigInt clones v and rejects negative values.
func NewRawAmountFromBigInt(v *big.Int) (RawAmount, error) {
	if v == nil || v.Sign() == 0 {
		return ZeroRawAmount(), nil
	}
	if v.Sign() < 0 {
		return ZeroRawAmount(), errNegativeRawAmount
	}
	return RawAmount{value: new(big.Int).Set(v)}, nil
}

// NewRawAmountFromString parses a canonical non-negative base-10 integer.
// Accepted examples: "0", "1", "1000000000000000000".
// Whitespace, signs, leading zeroes, exponents and decimal fractions are
// rejected so the same value always has one representation in JSON and JWS.
func NewRawAmountFromString(s string) (RawAmount, error) {
	if !isCanonicalUnsignedInteger(s) {
		return ZeroRawAmount(), fmt.Errorf("invalid raw amount %q: expected canonical non-negative integer", s)
	}
	if s == "0" {
		return ZeroRawAmount(), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return ZeroRawAmount(), fmt.Errorf("invalid raw amount %q", s)
	}
	return RawAmount{value: v}, nil
}

// MustRawAmount parses a constant or panics. It is intended for constants
// and tests only.
func MustRawAmount(s string) RawAmount {
	a, err := NewRawAmountFromString(s)
	if err != nil {
		panic(err)
	}
	return a
}

// Add returns a+b without changing either operand.
func (a RawAmount) Add(b RawAmount) RawAmount {
	return rawAmountFromNonNegative(new(big.Int).Add(a.bigInt(), b.bigInt()))
}

// Sub returns a-b without changing either operand. It panics on underflow;
// callers that need a signed result must use Difference.
func (a RawAmount) Sub(b RawAmount) RawAmount {
	result := new(big.Int).Sub(a.bigInt(), b.bigInt())
	if result.Sign() < 0 {
		panic(fmt.Sprintf("raw amount underflow: %s - %s", a, b))
	}
	return rawAmountFromNonNegative(result)
}

// Difference returns the signed difference a-b.
func (a RawAmount) Difference(b RawAmount) RawDelta {
	return rawDeltaFromBigInt(new(big.Int).Sub(a.bigInt(), b.bigInt()))
}

// MulDiv computes floor(a*numerator/denominator) using arbitrary-precision
// integer arithmetic. Negative factors and a non-positive denominator are
// invalid for a non-negative raw amount and panic as programming errors.
func (a RawAmount) MulDiv(numerator, denominator int64) RawAmount {
	if numerator < 0 {
		panic("RawAmount.MulDiv: numerator cannot be negative")
	}
	if denominator <= 0 {
		panic("RawAmount.MulDiv: denominator must be positive")
	}
	product := new(big.Int).Mul(a.bigInt(), big.NewInt(numerator))
	return rawAmountFromNonNegative(product.Quo(product, big.NewInt(denominator)))
}

// Cmp compares a and b and returns -1, 0, or +1.
func (a RawAmount) Cmp(b RawAmount) int { return a.bigInt().Cmp(b.bigInt()) }

// Equal reports whether a and b contain the same number of raw units.
func (a RawAmount) Equal(b RawAmount) bool { return a.Cmp(b) == 0 }

// IsZero reports whether a is zero.
func (a RawAmount) IsZero() bool { return a.value == nil || a.value.Sign() == 0 }

// IsPositive reports whether a is greater than zero.
func (a RawAmount) IsPositive() bool { return a.value != nil && a.value.Sign() > 0 }

// BigInt returns an independent copy of the raw integer.
func (a RawAmount) BigInt() *big.Int { return a.bigInt() }

// Int64Exact converts a to int64 and returns an error rather than truncating.
// It is intended only for chain adapters whose external protocol is bounded to
// int64 (for example, the current TRON HTTP API).
func (a RawAmount) Int64Exact() (int64, error) {
	v := a.bigInt()
	if !v.IsInt64() {
		return 0, fmt.Errorf("raw amount %s does not fit int64", a)
	}
	return v.Int64(), nil
}

// ToDecimal applies asset metadata scale for display or conversion. The raw
// amount remains an integer.
func (a RawAmount) ToDecimal(decimalPlaces int) decimal.Decimal {
	return decimal.NewFromBigInt(a.bigInt(), -int32(decimalPlaces))
}

func (a RawAmount) String() string {
	if a.IsZero() {
		return "0"
	}
	return a.value.String()
}

// MarshalText implements encoding.TextMarshaler.
func (a RawAmount) MarshalText() ([]byte, error) { return []byte(a.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (a *RawAmount) UnmarshalText(text []byte) error {
	if a == nil {
		return errors.New("cannot unmarshal raw amount into nil receiver")
	}
	parsed, err := NewRawAmountFromString(string(text))
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// MarshalJSON always emits a quoted canonical decimal string.
func (a RawAmount) MarshalJSON() ([]byte, error) { return json.Marshal(a.String()) }

// MarshalJSONTo implements json.MarshalerTo without an intermediate buffer.
func (a RawAmount) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(a.String()))
}

// UnmarshalJSON accepts only a quoted canonical decimal string.
func (a *RawAmount) UnmarshalJSON(data []byte) error {
	if a == nil {
		return errors.New("cannot unmarshal raw amount into nil receiver")
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err != nil {
		return fmt.Errorf("raw amount must be a quoted decimal string: %w", err)
	}
	return a.UnmarshalText([]byte(encoded))
}

// UnmarshalJSONFrom implements json.UnmarshalerFrom.
func (a *RawAmount) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if a == nil {
		return errors.New("cannot unmarshal raw amount into nil receiver")
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '"' {
		return fmt.Errorf("raw amount must be a quoted decimal string")
	}
	return a.UnmarshalText([]byte(tok.String()))
}

// Value implements database/sql/driver.Valuer. PostgreSQL casts the canonical
// string to NUMERIC(78,0), avoiding a dependency from core types to pgx.
func (a RawAmount) Value() (driver.Value, error) { return a.String(), nil }

// Scan implements database/sql.Scanner for exact NUMERIC values. NULL and
// fractional values are rejected; nullable columns use the PostgreSQL adapter
// NullableRawAmount so NULL cannot be confused with zero.
func (a *RawAmount) Scan(src any) error {
	if a == nil {
		return errors.New("cannot scan raw amount into nil receiver")
	}
	if src == nil {
		return errors.New("cannot scan NULL into RawAmount")
	}

	var encoded string
	switch v := src.(type) {
	case int64:
		if v < 0 {
			return errNegativeRawAmount
		}
		*a = NewRawAmountFromInt64(v)
		return nil
	case string:
		encoded = v
	case []byte:
		encoded = string(v)
	default:
		return fmt.Errorf("cannot scan RawAmount from %T", src)
	}
	return a.UnmarshalText([]byte(encoded))
}

func (a RawAmount) bigInt() *big.Int {
	if a.value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(a.value)
}

func rawAmountFromNonNegative(v *big.Int) RawAmount {
	if v == nil || v.Sign() == 0 {
		return ZeroRawAmount()
	}
	if v.Sign() < 0 {
		panic(errNegativeRawAmount)
	}
	return RawAmount{value: new(big.Int).Set(v)}
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

// RawDelta is an immutable signed difference between two RawAmount
// values. It is used for reconciliation and incident evidence, never for
// business amounts or ledger movement resources.
type RawDelta struct {
	value *big.Int
	_     [0]func()
}

func ZeroRawDelta() RawDelta { return RawDelta{} }

func NewRawDeltaFromString(s string) (RawDelta, error) {
	if !isCanonicalSignedInteger(s) {
		return ZeroRawDelta(), fmt.Errorf("invalid raw delta %q: expected canonical integer", s)
	}
	if s == "0" {
		return ZeroRawDelta(), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return ZeroRawDelta(), fmt.Errorf("invalid raw delta %q", s)
	}
	return rawDeltaFromBigInt(v), nil
}

func (d RawDelta) Add(other RawDelta) RawDelta {
	return rawDeltaFromBigInt(new(big.Int).Add(d.bigInt(), other.bigInt()))
}

func (d RawDelta) Neg() RawDelta {
	return rawDeltaFromBigInt(new(big.Int).Neg(d.bigInt()))
}

func (d RawDelta) Abs() RawAmount {
	return rawAmountFromNonNegative(new(big.Int).Abs(d.bigInt()))
}

func (d RawDelta) Cmp(other RawDelta) int { return d.bigInt().Cmp(other.bigInt()) }
func (d RawDelta) IsZero() bool           { return d.value == nil || d.value.Sign() == 0 }
func (d RawDelta) IsPositive() bool       { return d.value != nil && d.value.Sign() > 0 }
func (d RawDelta) IsNegative() bool       { return d.value != nil && d.value.Sign() < 0 }
func (d RawDelta) BigInt() *big.Int       { return d.bigInt() }

func (d RawDelta) String() string {
	if d.IsZero() {
		return "0"
	}
	return d.value.String()
}

func (d RawDelta) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *RawDelta) UnmarshalText(text []byte) error {
	if d == nil {
		return errors.New("cannot unmarshal raw delta into nil receiver")
	}
	parsed, err := NewRawDeltaFromString(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d RawDelta) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d RawDelta) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(d.String()))
}

func (d *RawDelta) UnmarshalJSON(data []byte) error {
	if d == nil {
		return errors.New("cannot unmarshal raw delta into nil receiver")
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err != nil {
		return fmt.Errorf("raw delta must be a quoted decimal string: %w", err)
	}
	return d.UnmarshalText([]byte(encoded))
}

func (d *RawDelta) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if d == nil {
		return errors.New("cannot unmarshal raw delta into nil receiver")
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '"' {
		return fmt.Errorf("raw delta must be a quoted decimal string")
	}
	return d.UnmarshalText([]byte(tok.String()))
}

func (d RawDelta) Value() (driver.Value, error) { return d.String(), nil }

func (d *RawDelta) Scan(src any) error {
	if d == nil {
		return errors.New("cannot scan raw delta into nil receiver")
	}
	if src == nil {
		return errors.New("cannot scan NULL into RawDelta")
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
		return fmt.Errorf("cannot scan RawDelta from %T", src)
	}
	return d.UnmarshalText([]byte(encoded))
}

func (d RawDelta) bigInt() *big.Int {
	if d.value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(d.value)
}

func rawDeltaFromBigInt(v *big.Int) RawDelta {
	if v == nil || v.Sign() == 0 {
		return ZeroRawDelta()
	}
	return RawDelta{value: new(big.Int).Set(v)}
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

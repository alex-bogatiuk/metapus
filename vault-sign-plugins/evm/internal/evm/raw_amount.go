package evm

import (
	"fmt"
	"math/big"
	"strconv"
	"vault-plugin-secrets-evm/internal/json"
)

// RawAmount is a canonical, non-negative integer amount in blockchain raw
// units. The Vault plugin keeps its own small value type so the signed JWS
// contract has no dependency on the application module.
type RawAmount string

func NewRawAmountFromInt64(value int64) RawAmount {
	if value < 0 {
		panic("raw amount cannot be negative")
	}
	return RawAmount(strconv.FormatInt(value, 10))
}

func (a RawAmount) String() string { return string(a) }

func (a RawAmount) IsZero() bool { return a == "0" }

func (a RawAmount) IsPositive() bool {
	value, ok := a.bigInt()
	return ok && value.Sign() > 0
}

func (a RawAmount) Equal(other RawAmount) bool { return a == other }

func (a RawAmount) Cmp(other RawAmount) int {
	left, leftOK := a.bigInt()
	right, rightOK := other.bigInt()
	if !leftOK || !rightOK {
		panic("cannot compare invalid raw amounts")
	}
	return left.Cmp(right)
}

func (a RawAmount) BigInt() (*big.Int, error) {
	value, ok := a.bigInt()
	if !ok {
		return nil, fmt.Errorf("invalid raw amount %q", a)
	}
	return value, nil
}

func (a RawAmount) Int64Exact() (int64, error) {
	value, err := a.BigInt()
	if err != nil {
		return 0, err
	}
	if !value.IsInt64() {
		return 0, fmt.Errorf("raw amount %s does not fit int64", a)
	}
	return value.Int64(), nil
}

func (a RawAmount) MarshalJSON() ([]byte, error) {
	if _, ok := a.bigInt(); !ok {
		return nil, fmt.Errorf("invalid raw amount %q", a)
	}
	return json.Marshal(string(a))
}

func (a *RawAmount) UnmarshalJSON(data []byte) error {
	if a == nil {
		return fmt.Errorf("cannot unmarshal raw amount into nil receiver")
	}
	var encoded string
	if err := json.Unmarshal(data, &encoded); err != nil {
		return fmt.Errorf("raw amount must be a quoted decimal string: %w", err)
	}
	parsed := RawAmount(encoded)
	if _, ok := parsed.bigInt(); !ok {
		return fmt.Errorf("invalid raw amount %q", encoded)
	}
	*a = parsed
	return nil
}

func (a RawAmount) bigInt() (*big.Int, bool) {
	encoded := string(a)
	if encoded == "0" {
		return new(big.Int), true
	}
	if len(encoded) == 0 || encoded[0] < '1' || encoded[0] > '9' {
		return nil, false
	}
	for i := 1; i < len(encoded); i++ {
		if encoded[i] < '0' || encoded[i] > '9' {
			return nil, false
		}
	}
	value, ok := new(big.Int).SetString(encoded, 10)
	return value, ok
}

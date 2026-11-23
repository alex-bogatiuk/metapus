package types

import (
	"math/big"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

func TestRawAmount_ArbitraryPrecisionArithmetic(t *testing.T) {
	a := MustRawAmount("115792089237316195423570985008687907853269984665640564039457584007913129639935")
	b := MustRawAmount("1000000000000000000")

	got := a.Add(b).Sub(b)
	if !got.Equal(a) {
		t.Fatalf("round trip arithmetic = %s, want %s", got, a)
	}
	if got.String() != "115792089237316195423570985008687907853269984665640564039457584007913129639935" {
		t.Fatalf("unexpected value: %s", got)
	}
}

func TestRawAmount_ImmutableAtBigIntBoundaries(t *testing.T) {
	source, ok := new(big.Int).SetString("100000000000000000000000000000000000000", 10)
	if !ok {
		t.Fatal("invalid fixture")
	}
	a, err := NewRawAmountFromBigInt(source)
	if err != nil {
		t.Fatal(err)
	}

	source.SetInt64(1)
	copy := a.BigInt()
	copy.SetInt64(2)
	_ = a.Add(MustRawAmount("3"))

	if a.String() != "100000000000000000000000000000000000000" {
		t.Fatalf("amount mutated through a big.Int alias: %s", a)
	}
}

func TestRawAmount_ZeroValue(t *testing.T) {
	var a RawAmount
	if !a.IsZero() || a.String() != "0" {
		t.Fatalf("zero value = %q", a.String())
	}
	if a.BigInt().Sign() != 0 {
		t.Fatal("zero BigInt must have sign 0")
	}
}

func TestRawAmount_StrictCanonicalParsing(t *testing.T) {
	valid := []string{"0", "1", "100000000000000000000000000000000000000000000000000000000000000000000000000000"}
	for _, input := range valid {
		t.Run("valid_"+input, func(t *testing.T) {
			got, err := NewRawAmountFromString(input)
			if err != nil || got.String() != input {
				t.Fatalf("parse(%q) = %s, %v", input, got, err)
			}
		})
	}

	invalid := []string{"", "null", "-1", "+1", "00", "01", " 1", "1 ", "1.0", "1e3"}
	for _, input := range invalid {
		t.Run("invalid_"+input, func(t *testing.T) {
			if _, err := NewRawAmountFromString(input); err == nil {
				t.Fatalf("expected %q to be rejected", input)
			}
		})
	}
}

func TestRawAmount_JSONIsQuotedCanonicalString(t *testing.T) {
	original := MustRawAmount("100000000000000000000000000000000000000")
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `"100000000000000000000000000000000000000"` {
		t.Fatalf("encoded = %s", encoded)
	}

	var decoded RawAmount
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(original) {
		t.Fatalf("decoded = %s, want %s", decoded, original)
	}

	for _, invalid := range []string{`1`, `1.0`, `null`, `"01"`, `"-1"`} {
		if err := json.Unmarshal([]byte(invalid), &decoded); err == nil {
			t.Fatalf("expected JSON %s to be rejected", invalid)
		}
	}
}

func TestRawAmount_SubUnderflowPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected underflow panic")
		}
	}()
	MustRawAmount("1").Sub(MustRawAmount("2"))
}

func TestRawAmount_MulDivHasNoIntermediateOverflow(t *testing.T) {
	a := MustRawAmount("100000000000000000000000000000000000000")
	got := a.MulDiv(2500, 10000)
	if got.String() != "25000000000000000000000000000000000000" {
		t.Fatalf("MulDiv = %s", got)
	}
}

func TestRawAmount_Int64Exact(t *testing.T) {
	got, err := MustRawAmount("9223372036854775807").Int64Exact()
	if err != nil || got != int64(9223372036854775807) {
		t.Fatalf("Int64Exact = %d, %v", got, err)
	}
	if _, err := MustRawAmount("9223372036854775808").Int64Exact(); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestRawAmount_ToDecimal(t *testing.T) {
	if got := MustRawAmount("1000000").ToDecimal(6).String(); got != "1" {
		t.Fatalf("ToDecimal = %s", got)
	}
	if got := MustRawAmount("1000000000000000001").ToDecimal(18).String(); got != "1.000000000000000001" {
		t.Fatalf("wei ToDecimal = %s", got)
	}
}

func TestRawDelta_SignedDifferenceAndJSON(t *testing.T) {
	delta := MustRawAmount("1").Difference(MustRawAmount("100000000000000000000"))
	if delta.String() != "-99999999999999999999" || !delta.IsNegative() {
		t.Fatalf("delta = %s", delta)
	}
	if delta.Abs().String() != "99999999999999999999" {
		t.Fatalf("abs = %s", delta.Abs())
	}
	encoded, err := json.Marshal(delta)
	if err != nil || string(encoded) != `"-99999999999999999999"` {
		t.Fatalf("encoded delta = %s, %v", encoded, err)
	}
}

func TestRawAmount_SQLScanRejectsLossyValues(t *testing.T) {
	var amount RawAmount
	if err := amount.Scan("100000000000000000000000000000000000000"); err != nil {
		t.Fatal(err)
	}
	if amount.String() != "100000000000000000000000000000000000000" {
		t.Fatalf("scan = %s", amount)
	}
	for _, invalid := range []any{nil, 1.5, "1.0", "-1"} {
		if err := amount.Scan(invalid); err == nil {
			t.Fatalf("expected %#v to be rejected", invalid)
		}
	}
}

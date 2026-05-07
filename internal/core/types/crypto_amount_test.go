// internal/core/types/crypto_amount_test.go
package types

import (
	"encoding/json"
	"testing"
)

func TestCryptoAmount_Arithmetic(t *testing.T) {
	a := NewCryptoAmountFromInt64(1_000_000)
	b := NewCryptoAmountFromInt64(500_000)

	tests := []struct {
		give string
		fn   func() CryptoAmount
		want int64
	}{
		{"Add", func() CryptoAmount { return a.Add(b) }, 1_500_000},
		{"Sub", func() CryptoAmount { return a.Sub(b) }, 500_000},
		{"Neg", func() CryptoAmount { return a.Neg() }, -1_000_000},
		{"Abs of negative", func() CryptoAmount { return a.Neg().Abs() }, 1_000_000},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			got := tt.fn().Int64()
			if got != tt.want {
				t.Errorf("%s = %d, want %d", tt.give, got, tt.want)
			}
		})
	}
}

func TestCryptoAmount_OverflowPanics(t *testing.T) {
	tests := []struct {
		give string
		fn   func()
	}{
		{"Add overflow", func() { CryptoAmount(9_223_372_036_854_775_807).Add(1) }},
		{"Sub overflow", func() { CryptoAmount(-9_223_372_036_854_775_808).Sub(1) }},
		{"Neg MinInt64", func() { CryptoAmount(-9_223_372_036_854_775_808).Neg() }},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected panic, got none")
				}
			}()
			tt.fn()
		})
	}
}

func TestCryptoAmount_Comparison(t *testing.T) {
	zero := ZeroCryptoAmount()
	positive := NewCryptoAmountFromInt64(100)
	negative := NewCryptoAmountFromInt64(-100)

	tests := []struct {
		give string
		a    CryptoAmount
		fn   string
		want bool
	}{
		{"zero.IsZero", zero, "IsZero", true},
		{"positive.IsZero", positive, "IsZero", false},
		{"positive.IsPositive", positive, "IsPositive", true},
		{"zero.IsPositive", zero, "IsPositive", false},
		{"negative.IsNegative", negative, "IsNegative", true},
		{"zero.IsNegative", zero, "IsNegative", false},
		{"positive.IsNegative", positive, "IsNegative", false},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			var got bool
			switch tt.fn {
			case "IsZero":
				got = tt.a.IsZero()
			case "IsPositive":
				got = tt.a.IsPositive()
			case "IsNegative":
				got = tt.a.IsNegative()
			}
			if got != tt.want {
				t.Errorf("%s = %v, want %v", tt.give, got, tt.want)
			}
		})
	}
}

func TestCryptoAmount_Cmp(t *testing.T) {
	tests := []struct {
		give string
		a, b int64
		want int // -1, 0, +1
	}{
		{"equal", 100, 100, 0},
		{"a < b", 50, 100, -1},
		{"a > b", 200, 100, 1},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			a := NewCryptoAmountFromInt64(tt.a)
			b := NewCryptoAmountFromInt64(tt.b)
			got := a.Cmp(b)
			if got != tt.want {
				t.Errorf("Cmp(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCryptoAmount_Immutability(t *testing.T) {
	original := NewCryptoAmountFromInt64(1_000_000)
	_ = original.Add(NewCryptoAmountFromInt64(500_000))

	// Original should NOT change (int64 is a value type — inherently immutable)
	if original.Int64() != 1_000_000 {
		t.Errorf("original mutated: got %s, want 1000000", original.String())
	}
}

func TestCryptoAmount_ZeroValue(t *testing.T) {
	var a CryptoAmount // zero-value
	if !a.IsZero() {
		t.Error("zero-value CryptoAmount should be zero")
	}
	if a.String() != "0" {
		t.Errorf("String() = %q, want %q", a.String(), "0")
	}
}

func TestCryptoAmount_JSON_RoundTrip(t *testing.T) {
	tests := []struct {
		give string
		val  int64
	}{
		{"zero", 0},
		{"small positive", 1000},
		{"large (10 USDT)", 10_000_000},
		{"negative", -500},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			original := NewCryptoAmountFromInt64(tt.val)

			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var decoded CryptoAmount
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if decoded.Cmp(original) != 0 {
				t.Errorf("round-trip failed: got %s, want %s", decoded.String(), original.String())
			}
		})
	}
}

func TestCryptoAmount_JSON_NumberEncoding(t *testing.T) {
	// JSON should now encode as number (not string)
	a := NewCryptoAmountFromInt64(5_000_000)
	data, _ := json.Marshal(a)

	got := string(data)
	want := `5000000`
	if got != want {
		t.Errorf("JSON encoding = %s, want %s (number, not string)", got, want)
	}
}

func TestCryptoAmount_JSON_BackwardCompat(t *testing.T) {
	// Should still decode from old string format
	var a CryptoAmount
	if err := json.Unmarshal([]byte(`"2000000"`), &a); err != nil {
		t.Fatalf("Unmarshal from string failed: %v", err)
	}
	if a.Int64() != 2_000_000 {
		t.Errorf("got %d, want 2000000", a.Int64())
	}
}

func TestCryptoAmount_FromString(t *testing.T) {
	tests := []struct {
		give    string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"1000000", 1_000_000, false},
		{"-500", -500, false},
		{"not_a_number", 0, true},
		{"1.5", 0, true}, // not an integer
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			a, err := NewCryptoAmountFromString(tt.give)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Int64() != tt.want {
				t.Errorf("got %d, want %d", a.Int64(), tt.want)
			}
		})
	}
}

func TestCryptoAmount_ToDecimal(t *testing.T) {
	// 1 USDT = 1_000_000 minor units, 6 decimals
	a := NewCryptoAmountFromInt64(1_000_000)
	d := a.ToDecimal(6)

	got := d.String()
	want := "1"
	if got != want {
		t.Errorf("ToDecimal(6) = %s, want %s", got, want)
	}

	// 0.5 USDT = 500_000 minor units
	b := NewCryptoAmountFromInt64(500_000)
	d2 := b.ToDecimal(6)
	got2 := d2.String()
	want2 := "0.5"
	if got2 != want2 {
		t.Errorf("ToDecimal(6) = %s, want %s", got2, want2)
	}
}

package crypto

import (
	"context"
	"testing"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestBalanceCalculatorPreservesTokenDecimalPlaces(t *testing.T) {
	calc := NewBalanceCalculator(nil)

	tokenID := id.New()
	got, err := calc.Calculate(context.Background(), []BalanceRow{
		{
			TokenID:       tokenID,
			TokenSymbol:   "USDT",
			DecimalPlaces: 6,
			RawAmount:     types.NewCryptoAmountFromInt64(1_500_000),
		},
	}, id.New(), nil)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if len(got.ByToken) != 1 {
		t.Fatalf("len(ByToken) = %d, want 1", len(got.ByToken))
	}

	token := got.ByToken[0]
	if token.DecimalPlaces != 6 {
		t.Fatalf("DecimalPlaces = %d, want 6", token.DecimalPlaces)
	}
	if token.HumanAmount.StringFixed(6) != "1.500000" {
		t.Fatalf("HumanAmount = %s, want 1.500000", token.HumanAmount.StringFixed(6))
	}
}

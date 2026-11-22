package crypto

import (
	"metapus/pkg/sdk/registers/exchange_rate"
	"testing"
	"time"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

func TestBalanceCalculatorPreservesTokenDecimalPlaces(t *testing.T) {
	cfg := exchange_rate.Configuration{BaseCurrency: exchange_rate.CurrencyRef{ID: id.New(), Code: "USD", DecimalPlaces: 2}}

	tokenID := id.New()
	networkID := id.New()
	got := CalculateBalance([]BalanceRow{
		{
			TokenID:       tokenID,
			TokenSymbol:   "USDT",
			NetworkID:     networkID,
			Network:       "TRON Nile Testnet",
			DecimalPlaces: 6,
			RawAmount:     types.NewRawAmountFromInt64(1_500_000),
		},
	}, nil, nil, cfg, time.Now())
	if len(got.ByToken) != 1 {
		t.Fatalf("len(ByToken) = %d, want 1", len(got.ByToken))
	}

	token := got.ByToken[0]
	if token.NetworkID != networkID.String() || token.Network != "TRON Nile Testnet" {
		t.Fatalf("network metadata was lost: %+v", token)
	}
	if token.DecimalPlaces != 6 {
		t.Fatalf("DecimalPlaces = %d, want 6", token.DecimalPlaces)
	}
	if token.HumanAmount.StringFixed(6) != "1.500000" {
		t.Fatalf("HumanAmount = %s, want 1.500000", token.HumanAmount.StringFixed(6))
	}
}

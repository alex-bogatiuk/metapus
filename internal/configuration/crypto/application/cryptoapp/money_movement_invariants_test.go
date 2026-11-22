package cryptoapp

import (
	"testing"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/types"
)

func TestParseHumanTokenAmountUsesExactMinorUnits(t *testing.T) {
	amount, err := parseHumanTokenAmount("12.345678", 6)
	if err != nil {
		t.Fatalf("parse exact decimal amount: %v", err)
	}
	if amount.String() != "12345678" {
		t.Fatalf("expected 12345678 minor units, got %s", amount.String())
	}

	for _, raw := range []string{"0", "-1", "1.0000001", "1e-7", "not-a-number"} {
		if _, err := parseHumanTokenAmount(raw, 6); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestMoneyMovementIntentMatchesEveryEconomicField(t *testing.T) {
	basisID, tokenID, merchantID, addressID := id.New(), id.New(), id.New(), id.New()
	base := crypto.MoneyMovementIntent{
		SchemaVersion: crypto.MoneyMovementIntentV1,
		TenantID:      "tenant-a", Purpose: crypto.SigningPurposeWithdrawal,
		BasisType: "withdrawal_request", BasisID: basisID, MerchantID: &merchantID,
		NetworkCode: "ethereum-sepolia", TokenID: tokenID, TokenStandard: "erc20",
		ContractAddress:    "0x1111111111111111111111111111111111111111",
		AmountMinorUnits:   types.NewRawAmountFromInt64(5000000),
		DestinationAddress: "0x2222222222222222222222222222222222222222", AddressID: addressID,
		SourceAddress: "0x3333333333333333333333333333333333333333", SourceVaultKeyID: "vault-key-a",
	}
	if !MoneyMovementIntentMatches(base, base) {
		t.Fatal("identical intents must match")
	}

	tests := map[string]func(*crypto.MoneyMovementIntent){
		"tenant":   func(v *crypto.MoneyMovementIntent) { v.TenantID = "tenant-b" },
		"purpose":  func(v *crypto.MoneyMovementIntent) { v.Purpose = crypto.SigningPurposeSweep },
		"basis":    func(v *crypto.MoneyMovementIntent) { v.BasisID = id.New() },
		"merchant": func(v *crypto.MoneyMovementIntent) { other := id.New(); v.MerchantID = &other },
		"network":  func(v *crypto.MoneyMovementIntent) { v.NetworkCode = "tron-nile" },
		"token":    func(v *crypto.MoneyMovementIntent) { v.TokenID = id.New() },
		"contract": func(v *crypto.MoneyMovementIntent) { v.ContractAddress = "0x4444444444444444444444444444444444444444" },
		"amount":   func(v *crypto.MoneyMovementIntent) { v.AmountMinorUnits = types.NewRawAmountFromInt64(5000001) },
		"destination": func(v *crypto.MoneyMovementIntent) {
			v.DestinationAddress = "0x5555555555555555555555555555555555555555"
		},
		"address id": func(v *crypto.MoneyMovementIntent) { v.AddressID = id.New() },
		"source":     func(v *crypto.MoneyMovementIntent) { v.SourceAddress = "0x6666666666666666666666666666666666666666" },
		"vault key":  func(v *crypto.MoneyMovementIntent) { v.SourceVaultKeyID = "vault-key-b" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if MoneyMovementIntentMatches(base, changed) {
				t.Fatalf("changed %s still matched immutable intent", name)
			}
		})
	}
}

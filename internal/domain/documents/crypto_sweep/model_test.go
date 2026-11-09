package crypto_sweep

import (
	"context"
	"testing"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestCryptoSweepPlatformCostDoesNotCreateCryptoFeeMovement(t *testing.T) {
	tokenID := id.New()
	hotWalletID := id.New()
	networkID := id.New()
	costTokenID := id.New()

	sweep := NewCryptoSweep(tokenID, hotWalletID)
	sweep.TotalFee = types.NewCryptoAmountFromInt64(4_000_000)
	sweep.PlatformCostLines = []CryptoSweepPlatformCostLine{
		{
			NetworkID:       networkID,
			CostTokenID:     costTokenID,
			CostType:        entity.PlatformCostTypeChainResourceRent,
			Provider:        "itrx",
			ExternalOrderID: "serial-1",
			Amount:          types.NewCryptoAmountFromInt64(4_000_000),
		},
	}

	feeMovements, err := sweep.GenerateCryptoFeeMovements(context.Background())
	if err != nil {
		t.Fatalf("GenerateCryptoFeeMovements() error: %v", err)
	}
	if len(feeMovements) != 0 {
		t.Fatalf("GenerateCryptoFeeMovements() len = %d, want 0", len(feeMovements))
	}

	costMovements, err := sweep.GenerateCryptoPlatformCostMovements(context.Background())
	if err != nil {
		t.Fatalf("GenerateCryptoPlatformCostMovements() error: %v", err)
	}
	if len(costMovements) != 1 {
		t.Fatalf("GenerateCryptoPlatformCostMovements() len = %d, want 1", len(costMovements))
	}

	got := costMovements[0]
	if got.RecordType != entity.RecordTypeExpense {
		t.Fatalf("RecordType = %s, want %s", got.RecordType, entity.RecordTypeExpense)
	}
	if got.Amount.String() != "4000000" {
		t.Fatalf("Amount = %s, want 4000000", got.Amount.String())
	}
	if got.CostTokenID != costTokenID {
		t.Fatalf("CostTokenID = %s, want %s", got.CostTokenID, costTokenID)
	}
}

func TestSweepV2NeverCreditsPrincipalAtBroadcast(t *testing.T) {
	s := NewCryptoSweep(id.New(), id.New())
	s.TotalAmount = types.NewCryptoAmountFromInt64(23)
	s.Lines = []CryptoSweepLine{{WalletID: id.New(), Amount: s.TotalAmount, TxHash: "broadcast"}}
	rows, err := s.GenerateCryptoBalanceMovements(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("v2 broadcast principal: %v %v", rows, err)
	}
	s.AccountingModelVersion = 1
	rows, err = s.GenerateCryptoBalanceMovements(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("legacy broadcast must preserve its original pair: %v %v", rows, err)
	}
}

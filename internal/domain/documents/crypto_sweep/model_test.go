package crypto_sweep

import (
	"context"
	"testing"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/domain/registers/cryptoposting"

	"github.com/stretchr/testify/require"
)

func TestCryptoSweepPlatformCostDoesNotCreateCryptoFeeMovement(t *testing.T) {
	tokenID := id.New()
	hotWalletID := id.New()
	networkID := id.New()
	costTokenID := id.New()

	sweep := NewCryptoSweep(tokenID, hotWalletID)
	sweep.TotalFee = types.NewRawAmountFromInt64(4_000_000)
	sweep.PlatformCostLines = []CryptoSweepPlatformCostLine{
		{
			NetworkID:       networkID,
			CostTokenID:     costTokenID,
			CostType:        crypto_platform_cost.PlatformCostTypeChainResourceRent,
			Provider:        "itrx",
			ExternalOrderID: "serial-1",
			Amount:          types.NewRawAmountFromInt64(4_000_000),
		},
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

func TestSweepPostingNeverMovesPrincipal(t *testing.T) {
	// Each line is recognized from its own final proof, never by posting.
	var doc any = NewCryptoSweep(id.New(), id.New())
	_, balance := doc.(cryptoposting.CryptoBalanceMovementSource)
	_, fee := doc.(cryptoposting.CryptoFeeMovementSource)
	require.False(t, balance)
	require.False(t, fee)
}

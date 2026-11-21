package crypto_fee

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

type feeRepoStub struct {
	balances []BalanceEntry
}

func (r *feeRepoStub) CreateMovements(ctx context.Context, movements []CryptoFeeMovement) error {
	return nil
}

func (r *feeRepoStub) AppendReversalsByRecorder(ctx context.Context, recorderID id.ID, beforeVersion int) error {
	return nil
}

func (r *feeRepoStub) GetMovementsByRecorder(ctx context.Context, recorderID id.ID) ([]CryptoFeeMovement, error) {
	return nil, nil
}

func (r *feeRepoStub) GetBalancesForUpdate(ctx context.Context, keys []BalanceKey) ([]BalanceEntry, error) {
	return r.balances, nil
}

func TestCheckAndReserveCryptoFeeRejectsInsufficientBalance(t *testing.T) {
	merchantID := id.New()
	tokenID := id.New()
	svc := NewService(&feeRepoStub{
		balances: []BalanceEntry{
			{
				MerchantID: merchantID,
				TokenID:    tokenID,
				FeeType:    FeeTypeProcessing,
				Balance:    types.NewRawAmountFromInt64(100),
			},
		},
	})

	err := svc.CheckAndReserveCryptoFee(context.Background(), []BalanceReservation{
		{
			MerchantID: merchantID,
			TokenID:    tokenID,
			FeeType:    FeeTypeProcessing,
			Required:   types.NewRawAmountFromInt64(101),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient crypto platform revenue")
}

func TestCheckAndReserveCryptoFeeAllowsSufficientBalance(t *testing.T) {
	merchantID := id.New()
	tokenID := id.New()
	svc := NewService(&feeRepoStub{
		balances: []BalanceEntry{
			{
				MerchantID: merchantID,
				TokenID:    tokenID,
				FeeType:    FeeTypeProcessing,
				Balance:    types.NewRawAmountFromInt64(100),
			},
		},
	})

	err := svc.CheckAndReserveCryptoFee(context.Background(), []BalanceReservation{
		{
			MerchantID: merchantID,
			TokenID:    tokenID,
			FeeType:    FeeTypeProcessing,
			Required:   types.NewRawAmountFromInt64(100),
		},
	})
	require.NoError(t, err)
}

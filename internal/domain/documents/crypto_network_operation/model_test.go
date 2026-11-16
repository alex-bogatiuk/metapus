package crypto_network_operation

import (
	"context"
	"testing"
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/registers/crypto_platform_cost"

	"github.com/stretchr/testify/require"
)

func finalActivation(cost types.RawAmount) *CryptoNetworkOperation {
	doc := NewWalletActivation(id.New(), id.New(), id.New(), id.New(), id.New(), "TTarget", "0xtx")
	doc.CostAmount = cost
	doc.Status = StatusSucceeded
	settlementID, reservationID := id.New(), id.New()
	now := time.Now().UTC()
	doc.ChainSettlementID = &settlementID
	doc.NetworkFeeReservationID = &reservationID
	doc.FinalizedAt = &now
	return doc
}

func TestFinalActivationRequiresItsFeeReservationAndNeverUnposts(t *testing.T) {
	doc := finalActivation(types.NewRawAmountFromInt64(10))
	require.NoError(t, doc.Validate(context.Background()))
	require.Error(t, doc.CanUnpost(context.Background()))
	doc.NetworkFeeReservationID = nil
	require.Error(t, doc.Validate(context.Background()))
	doc = finalActivation(types.NewRawAmountFromInt64(10))
	doc.FinalizedAt = nil
	_, err := doc.GenerateCryptoBalanceMovements(context.Background())
	require.Error(t, err)
}

func TestWalletActivationGeneratesBothExpenses(t *testing.T) {
	doc := finalActivation(types.NewRawAmountFromInt64(1250))
	require.Equal(t, ActivationTaskBasisType, doc.BasisType)
	require.NotNil(t, doc.BasisID)

	balance, err := doc.GenerateCryptoBalanceMovements(context.Background())
	require.NoError(t, err)
	require.Len(t, balance, 1)
	require.Equal(t, entity.RecordTypeExpense, balance[0].RecordType)
	require.Equal(t, doc.SourceWalletID, balance[0].WalletID)
	require.Equal(t, doc.CostAmount, balance[0].Amount)
	require.Equal(t, *doc.FinalizedAt, balance[0].Period)

	costs, err := doc.GenerateCryptoPlatformCostMovements(context.Background())
	require.NoError(t, err)
	require.Len(t, costs, 1)
	require.Equal(t, crypto_platform_cost.PlatformCostTypeChainNetworkFee, costs[0].CostType)
	require.Equal(t, crypto_platform_cost.PlatformCostPurposeActivation, costs[0].Purpose)
	require.Equal(t, "chain", costs[0].Provider)
	require.Equal(t, doc.TxHash, costs[0].ExternalOrderID)
}

func TestWalletActivationRequiresTechnicalTaskBasis(t *testing.T) {
	doc := finalActivation(types.NewRawAmountFromInt64(10))
	doc.BasisType = "LegacyActivationDocument"

	require.Error(t, doc.Validate(context.Background()))
	require.Error(t, doc.CanPost(context.Background()))
}

func TestWalletActivationZeroCostHasNoMovementsButCanPost(t *testing.T) {
	doc := finalActivation(types.ZeroRawAmount())

	balance, err := doc.GenerateCryptoBalanceMovements(context.Background())
	require.NoError(t, err)
	require.Empty(t, balance)
	costs, err := doc.GenerateCryptoPlatformCostMovements(context.Background())
	require.NoError(t, err)
	require.Empty(t, costs)
	require.NoError(t, doc.CanPost(context.Background()))
}

func TestUnknownOperationCannotValidateOrGenerateMovements(t *testing.T) {
	doc := finalActivation(types.NewRawAmountFromInt64(10))
	doc.OperationType = OperationType("future_operation")

	require.Error(t, doc.Validate(context.Background()))
	_, err := doc.GenerateCryptoBalanceMovements(context.Background())
	require.Error(t, err)
	_, err = doc.GenerateCryptoPlatformCostMovements(context.Background())
	require.Error(t, err)
}

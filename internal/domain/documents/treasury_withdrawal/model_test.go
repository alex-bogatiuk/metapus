package treasury_withdrawal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestTreasuryWithdrawalAdminStillCannotApproveOwnOperation(t *testing.T) {
	actor := id.New()
	doc := NewTreasuryWithdrawal(id.New(), id.New(), id.New(), "external", types.NewCryptoAmountFromInt64(1))
	doc.CreatedBy = actor
	doc.ApprovedBy = &actor
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: actor.String(), IsAdmin: true, Roles: []string{"admin"}})
	if err := doc.Validate(ctx); err == nil {
		t.Fatal("provider opening exception must not apply to treasury")
	}
}

func TestTreasuryV2BroadcastHasNoPrincipalOrRevenueExpense(t *testing.T) {
	doc := NewTreasuryWithdrawal(id.New(), id.New(), id.New(), "external", types.NewCryptoAmountFromInt64(23))
	doc.FeeLines = []FeeLine{{MerchantID: id.New(), TokenID: doc.TokenID, FeeType: entity.FeeTypeProcessing, Amount: doc.Amount}}
	principal, err := doc.GenerateCryptoBalanceMovements(context.Background())
	require.NoError(t, err)
	require.Empty(t, principal)
	revenue, err := doc.GenerateCryptoFeeMovements(context.Background())
	require.NoError(t, err)
	require.Empty(t, revenue)
	doc.AccountingModelVersion = 1
	principal, err = doc.GenerateCryptoBalanceMovements(context.Background())
	require.NoError(t, err)
	require.Len(t, principal, 1)
	revenue, err = doc.GenerateCryptoFeeMovements(context.Background())
	require.NoError(t, err)
	require.Len(t, revenue, 1)
}

func TestTreasuryWithdrawalCanPostRequiresBroadcastTxHash(t *testing.T) {
	doc := NewTreasuryWithdrawal(
		id.New(),
		id.New(),
		id.New(),
		"T9yD14Nj9j7xAB4dbGeiX9h8unkKHxuWwb",
		types.NewCryptoAmountFromInt64(100),
	)

	if err := doc.CanPost(context.Background()); err == nil {
		t.Fatal("pending treasury withdrawal must not be postable")
	}

	doc.Status = StatusBroadcast
	if err := doc.CanPost(context.Background()); err == nil {
		t.Fatal("broadcast treasury withdrawal without tx hash must not be postable")
	}

	doc.TxHash = "tx_hash"
	if err := doc.CanPost(context.Background()); err != nil {
		t.Fatalf("broadcast treasury withdrawal with tx hash should be postable: %v", err)
	}
}

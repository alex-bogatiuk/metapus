package treasury_withdrawal

import (
	"context"
	"testing"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"

	"github.com/stretchr/testify/require"
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

func TestTreasuryWithdrawalPostingCreatesNoPrincipalOrRevenueExpense(t *testing.T) {
	// Principal and revenue are recognized only at final chain settlement.
	var doc any = NewTreasuryWithdrawal(id.New(), id.New(), id.New(), "external", types.NewCryptoAmountFromInt64(23))
	_, balance := doc.(posting.CryptoBalanceMovementSource)
	_, fee := doc.(posting.CryptoFeeMovementSource)
	require.False(t, balance)
	require.False(t, fee)
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

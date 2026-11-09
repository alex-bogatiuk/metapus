package register_repo

import (
	"github.com/stretchr/testify/require"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"testing"
	"time"
)

func TestMovementPlansAreDeterministicAndStampedOnlyOnStorageCopy(t *testing.T) {
	recorder, wallet, token := id.New(), id.New(), id.New()
	at := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	generate := func() entity.CryptoBalanceMovement {
		return entity.NewCryptoBalanceMovement(recorder, "CryptoPayment", 1, at, entity.RecordTypeReceipt, wallet, token, types.NewCryptoAmountFromInt64(42))
	}
	a, b := generate(), generate()
	require.Equal(t, a, b)
	require.True(t, id.IsNil(a.LineID))
	require.True(t, a.CreatedAt.IsZero())
	plan := []entity.CryptoBalanceMovement{a, b}
	stored := stampMovementMetadata(plan, at)
	require.NotEqual(t, stored[0].LineID, stored[1].LineID)
	require.False(t, id.IsNil(stored[0].LineID))
	require.Equal(t, at, stored[0].CreatedAt)
	require.Equal(t, a, plan[0], "persistence must not stamp the reusable plan")
	require.Equal(t, stored, stampMovementMetadata(stored, at.Add(time.Hour)), "prepared identities/timestamps are retained")
}

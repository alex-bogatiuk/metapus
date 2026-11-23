//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
)

func TestUserCardsAndActiveAdmins(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := NewTxManagerFromRawPool(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)

	rollback := errors.New("rollback user lookup fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		named, firstOnly, blank, inactive, marked := id.New(), id.New(), id.New(), id.New(), id.New()
		users := []struct {
			id                            id.ID
			first, last                   *string
			admin, active, deletionMarked bool
		}{
			{named, ptr("Анна"), ptr("Иванова"), true, true, false},
			{firstOnly, ptr("Пётр"), nil, false, true, false},
			{blank, ptr(" "), ptr(""), true, true, false},
			{inactive, nil, nil, true, false, false},
			{marked, ptr("Удалён"), nil, true, true, true},
		}
		for _, u := range users {
			_, err := q.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash,first_name,last_name,is_admin,is_active,deletion_mark) VALUES($1,$2,'x',$3,$4,$5,$6,$7)`,
				u.id, email(u.id), u.first, u.last, u.admin, u.active, u.deletionMarked)
			require.NoError(t, err)
		}

		cards, err := UserCards(ctx, []id.ID{named, firstOnly, blank, inactive, marked, id.New()})
		require.NoError(t, err)
		require.Len(t, cards, 4, "a user marked for deletion or missing has no card")
		require.Equal(t, UserCard{ID: named, Email: email(named), DisplayName: "Анна Иванова"}, cards[named])
		require.Equal(t, "Пётр", cards[firstOnly].DisplayName)
		require.Equal(t, email(blank), cards[blank].DisplayName, "a blank name falls back to the email")
		require.Equal(t, email(inactive), cards[inactive].DisplayName)
		empty, err := UserCards(ctx, nil)
		require.NoError(t, err)
		require.Empty(t, empty)

		admins, err := ActiveAdminIDs(ctx)
		require.NoError(t, err)
		require.True(t, slices.IsSortedFunc(admins, func(a, b id.ID) int { return compareIDs(a, b) }))
		require.Contains(t, admins, named)
		require.Contains(t, admins, blank)
		for _, excluded := range []id.ID{firstOnly, inactive, marked} {
			require.NotContains(t, admins, excluded)
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

func ptr(s string) *string { return &s }

func email(user id.ID) string { return "card-" + id.Compact(user) + "@example.test" }

func compareIDs(a, b id.ID) int {
	switch {
	case a.String() < b.String():
		return -1
	case a.String() > b.String():
		return 1
	}
	return 0
}

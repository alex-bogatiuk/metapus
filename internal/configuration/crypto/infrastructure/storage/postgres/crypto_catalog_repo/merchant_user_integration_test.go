//go:build integration

package crypto_catalog_repo_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_catalog_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/sdktest"
	"metapus/pkg/sdk/tenant"
)

// A merchant's users carry the email and name the platform shows for them;
// a user marked for deletion keeps the membership without them.
func TestMerchantUsersCarryThePlatformUserCard(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn)
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := sdktest.NewTxManager(pool)
	ctx := tenant.WithTxManager(context.Background(), txm)

	rollback := errors.New("rollback merchant user fixture")
	err = txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		merchantID, owner, viewer := id.New(), id.New(), id.New()
		_, err := q.Exec(ctx, `INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Merchant users')`, merchantID, "mu-"+id.Compact(merchantID))
		require.NoError(t, err)
		_, err = q.Exec(ctx, `INSERT INTO auth_users(id,email,password_hash,first_name,last_name) VALUES($1,'owner-'||$3||'@example.test','x','Анна','Иванова'),($2,'viewer-'||$3||'@example.test','x',NULL,NULL)`,
			owner, viewer, id.Compact(merchantID))
		require.NoError(t, err)
		repo := crypto_catalog_repo.NewMerchantUserRepo()
		require.NoError(t, repo.Add(ctx, owner, merchantID, merchant.MerchantRole(1)))
		require.NoError(t, repo.Add(ctx, viewer, merchantID, merchant.MerchantRole(3)))

		users, err := repo.ListByMerchant(ctx, merchantID)
		require.NoError(t, err)
		require.Len(t, users, 2)
		byID := map[id.ID]merchant.MerchantUser{users[0].UserID: users[0], users[1].UserID: users[1]}
		require.Equal(t, "Анна Иванова", byID[owner].UserFullName)
		require.Equal(t, "owner-"+id.Compact(merchantID)+"@example.test", byID[owner].UserEmail)
		require.Equal(t, byID[viewer].UserEmail, byID[viewer].UserFullName, "a user without a name is shown by email")

		_, err = q.Exec(ctx, `UPDATE auth_users SET deletion_mark=TRUE WHERE id=$1`, viewer)
		require.NoError(t, err)
		users, err = repo.ListByMerchant(ctx, merchantID)
		require.NoError(t, err)
		require.Len(t, users, 2)
		for _, mu := range users {
			if mu.UserID == viewer {
				require.Empty(t, mu.UserEmail)
				require.Empty(t, mu.UserFullName)
			}
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

//go:build integration

package portal_repo

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/tenant"
)

func TestWithdrawalAllowlistRejectsManagedCustodyDestination(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_DATABASE_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	merchantID, networkID, walletID := id.New(), id.New(), id.New()
	suffix := id.Compact(networkID)
	managedAddress := "TManagedWithdrawal" + suffix
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_merchants(id,code,name) VALUES($1,$2,'Custody boundary test');
		INSERT INTO cat_blockchain_networks(
			id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
		VALUES($3,$4,'Custody boundary network','tron',$5,'TRX',6);
		INSERT INTO cat_wallets(
			id,code,name,network_id,address,tier,wallet_role,status,is_active,chain_activation_status)
		VALUES($6,$7,'Managed destination',$3,$8,'hot','liquidity_hot','free',TRUE,'active')
	`, pgx.QueryExecModeSimpleProtocol, merchantID, "CUSTODY-MERCHANT-"+suffix, networkID, "CUSTODY-NET-"+suffix,
		"custody-"+suffix, walletID, "CUSTODY-WALLET-"+suffix, managedAddress)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM reg_withdrawal_addresses WHERE merchant_id=$1`, merchantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_wallets WHERE id=$1`, walletID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_blockchain_networks WHERE id=$1`, networkID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM cat_merchants WHERE id=$1`, merchantID)
	})

	repo := NewDashboardRepo()
	_, err = repo.AddWithdrawalAddress(ctx, merchantID, networkID, managedAddress, managedAddress, "must fail")
	require.ErrorContains(t, err, "external to managed custody")

	_, err = pool.Exec(ctx, `
		INSERT INTO reg_withdrawal_addresses(merchant_id,network_id,address,label)
		VALUES($1,$2,$3,'trigger must fail')
	`, merchantID, networkID, managedAddress)
	require.Error(t, err)

	externalAddress := "TExternalWithdrawal" + suffix
	createdID, err := repo.AddWithdrawalAddress(ctx, merchantID, networkID, externalAddress, externalAddress, "external")
	require.NoError(t, err)
	require.NotEmpty(t, createdID)
}

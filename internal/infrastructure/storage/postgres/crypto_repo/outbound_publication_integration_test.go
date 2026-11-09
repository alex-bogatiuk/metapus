//go:build integration

package crypto_repo_test

import (
	"context"
	"testing"

	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"

	"github.com/stretchr/testify/require"
)

func TestAMLLineAuthorizationPublicationUsesExactLineBasis(t *testing.T) {
	f := crypto_repo.NewOutboundPublicationFixture(t)
	require.NotEqual(t, f.Parent, f.Auth.BasisID)
	var before string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, "SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=(SELECT token_id FROM doc_withdrawal_requests WHERE id=$2)", f.Auth.MerchantID, f.Parent).Scan(&before))
	f.AssertLockOrder(t,
		func(ctx context.Context) error {
			return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.Source)
		},
		func(ctx context.Context) error {
			_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, "SELECT id FROM reg_crypto_aml_withdrawal_lines WHERE id=$1 FOR UPDATE", f.Auth.BasisID)
			return err
		}, func(ctx context.Context) error {
			_, err := outbound_repo.NewOutboundRepo().InsertSigningAuthorization(ctx, f.Auth, "synthetic-jws", f.Unsigned, f.Bundle)
			return err
		})
	var count int
	var after string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, "SELECT count(*) FROM reg_crypto_signing_authorizations WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1", f.Auth.BasisID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, f.Pool.QueryRow(f.Ctx, "SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=(SELECT token_id FROM doc_withdrawal_requests WHERE id=$2)", f.Auth.MerchantID, f.Parent).Scan(&after))
	require.Equal(t, before, after, "publication must not create another merchant debit")
}

func TestAMLLinePublicationAndCancellationPreserveSingleDebit(t *testing.T) {
	for _, order := range []string{"publish_first", "cancel_first"} {
		t.Run(order, func(t *testing.T) {
			f := crypto_repo.NewOutboundPublicationFixture(t)
			tm := postgres.MustGetTxManager(f.Ctx)
			publish := func() error {
				_, err := outbound_repo.NewOutboundRepo().InsertSigningAuthorization(f.Ctx, f.Auth, "synthetic-jws", f.Unsigned, f.Bundle)
				return err
			}
			cancel := func() error {
				return tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
					_, err := crypto_repo.NewAMLRepository().CancelAMLWithdrawalRemainder(ctx, *f.Auth.MerchantID, f.Parent, "cancel unsigned remainder")
					return err
				})
			}
			if order == "publish_first" {
				require.NoError(t, publish())
				require.Error(t, cancel())
			} else {
				require.NoError(t, cancel())
				require.Error(t, publish())
			}
			var authorizations, expenses, refunds int
			require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT
                (SELECT count(*) FROM reg_crypto_signing_authorizations WHERE basis_id=$1),
                (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$2 AND record_type='expense'),
                (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$2 AND record_type='receipt')`, f.Auth.BasisID, f.Parent).Scan(&authorizations, &expenses, &refunds))
			require.Equal(t, 1, expenses)
			if order == "publish_first" {
				require.Equal(t, 1, authorizations)
				require.Zero(t, refunds)
			} else {
				require.Zero(t, authorizations)
				require.Equal(t, 1, refunds)
			}
		})
	}
}

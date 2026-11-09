//go:build integration

package crypto_repo_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/outbound_repo"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

func TestSweepExpiryChecksCancellationPayloadAndRollsBackWholeBatch(t *testing.T) {
	for _, resource := range []bool{false, true} {
		name := "allocation"
		if resource {
			name = "resource"
		}
		for _, change := range []string{"reason", "actor", "matching"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				f := crypto_repo.NewSweepCancellationFixture(t, resource)
				reason, actor := f.Reason, "sweep_lifecycle"
				if change == "reason" {
					reason = "original manual cancellation"
				}
				if change == "actor" {
					actor = "original operator"
				}
				_, err := f.Pool.Exec(f.Ctx, `INSERT INTO `+f.Table+`(`+f.Column+`,reason,cancelled_by) VALUES($1,$2,$3)`, f.Key, reason, actor)
				require.NoError(t, err)
				var before, after string
				require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(c)::text FROM `+f.Table+` c WHERE `+f.Column+`=$1`, f.Key).Scan(&before))
				err = outbound_repo.NewOutboundRepo().ExpireStaleSweepAttempts(f.Ctx)
				var auth, state string
				require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT a.status,b.status FROM reg_crypto_signing_authorizations a CROSS JOIN `+f.StateTable+` b WHERE a.id=$1 AND b.id=$2`, f.Auth, f.Key).Scan(&auth, &state))
				if change == "matching" {
					require.NoError(t, err)
					require.Equal(t, []string{"failed", "cancelled"}, []string{auth, state})
					require.NoError(t, outbound_repo.NewOutboundRepo().ExpireStaleSweepAttempts(f.Ctx))
				} else {
					require.ErrorContains(t, err, "original reason or actor")
					require.Equal(t, []string{"created", "reserved"}, []string{auth, state}, "the preceding authorization update must roll back")
				}
				require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(c)::text FROM `+f.Table+` c WHERE `+f.Column+`=$1`, f.Key).Scan(&after))
				require.Equal(t, before, after)
			})
		}
	}
}

func TestAMLPortalRejectionLocksWalletBeforeParentRow(t *testing.T) {
	f := crypto_repo.NewOutboundPublicationFixture(t)
	tm := postgres.MustGetTxManager(f.Ctx)
	f.AssertLockOrder(t,
		func(ctx context.Context) error {
			return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, f.Source)
		},
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_withdrawal_requests WHERE id=$1 FOR UPDATE`, f.Parent)
			return err
		}, func(ctx context.Context) error {
			_, _, _, err := portal_repo.NewDashboardRepo().LockWithdrawalRequestForRejection(ctx, *f.Auth.MerchantID, f.Parent)
			return err
		})
}

func TestSweepMaintenanceRejectsRepeatableReadSnapshot(t *testing.T) {
	f := crypto_repo.NewSweepCancellationFixture(t, false)
	tm := postgres.MustGetTxManager(f.Ctx)
	options := postgres.DefaultTxOptions()
	options.IsolationLevel = pgx.RepeatableRead
	err := tm.RunInTransactionWithOptions(f.Ctx, options, func(ctx context.Context) error {
		_, err := outbound_repo.NewOutboundRepo().PrepareSweepMaintenance(ctx)
		return err
	})
	require.ErrorContains(t, err, "read committed")
}

func TestAMLAdminRejectionConcurrentCommandsCompensateOnce(t *testing.T) {
	f := crypto_repo.NewOutboundPublicationFixture(t, true)
	service := cryptoapp.NewWithdrawalRejectionService(portal_repo.NewDashboardRepo(), crypto_merchant_balance.NewService(register_repo.NewCryptoMerchantBalanceRepo()))
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { errs[i] = service.Reject(f.Ctx, f.Parent, id.New(), "reject unsigned request") })
	}
	wg.Wait()
	require.NotEqual(t, errs[0] == nil, errs[1] == nil)
	var status, balance string
	var refunds int
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT r.status,b.amount::text,
 (SELECT count(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=r.id AND record_type='receipt')
 FROM doc_withdrawal_requests r JOIN reg_crypto_merchant_balance_balances b ON b.merchant_id=r.merchant_id AND b.token_id=r.token_id WHERE r.id=$1`, f.Parent).Scan(&status, &balance, &refunds))
	require.Equal(t, "rejected", status)
	require.Equal(t, "90", balance)
	require.Equal(t, 1, refunds)
}

func TestAMLWithdrawalResourceFootprintIsRecheckedAfterScopeWait(t *testing.T) {
	f := crypto_repo.NewOutboundPublicationFixture(t)
	tm := postgres.MustGetTxManager(f.Ctx)
	native, payer := id.New(), id.New()
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO cat_tokens(id,code,name,network_id,symbol,decimal_places,token_standard)
 VALUES($1,$1::uuid::text,'Synthetic fee asset',$3,'TRX',6,'native');
 INSERT INTO cat_wallets(id,code,name,network_id,custody_merchant_id,address,vault_key_id,wallet_role)
 VALUES($2,$2::uuid::text,'Synthetic fee payer',$3,$4,$2::uuid::text,'test-key','network_fee')`, pgx.QueryExecModeSimpleProtocol, native, payer, f.Auth.NetworkID, f.Auth.MerchantID)
	require.NoError(t, err)
	var prefixErr error
	f.AssertLockOrder(t,
		func(ctx context.Context) error {
			_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT version FROM sys_crypto_aml_routing_scopes WHERE merchant_id=$1 AND network_id=$2 FOR UPDATE`, f.Auth.MerchantID, f.Auth.NetworkID)
			return err
		}, func(ctx context.Context) error {
			// This creates only a synthetic operational reservation, never a fee
			// settlement or financial movement. Production availability is checked
			// by FeeManager before the same insert.
			_, err := tm.GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_fee_reservations(accounting_model_version,network_id,payer_wallet_id,native_token_id,operation_token_id,basis_type,basis_id,purpose,attempt,strategy,max_fee_minor)
 VALUES(1,$1,$2,$3,(SELECT token_id FROM doc_withdrawal_requests WHERE id=$4),'CryptoAMLWithdrawalLine',$5,'withdrawal',1,'native_burn',1)`, f.Auth.NetworkID, payer, native, f.Parent, f.Auth.BasisID)
			return err
		}, func(ctx context.Context) error {
			prefixErr = crypto_repo.NewAMLRepository().LockAMLWithdrawalRequest(ctx, f.Parent)
			return nil // No command writes follow a failed preflight.
		})
	require.ErrorContains(t, prefixErr, "footprint changed")
}

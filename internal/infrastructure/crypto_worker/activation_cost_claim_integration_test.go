//go:build integration

package crypto_worker

import (
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

func TestActivationCostRepairFencingPreservesFinancialOwner(t *testing.T) {
	for _, authoritative := range []bool{false, true} {
		name := "guarded repair"
		if authoritative {
			name = "authoritative finality revokes repair"
		}
		t.Run(name, func(t *testing.T) {
			f := newNetworkFeeFixture(t, crypto.SigningPurposeActivation)
			f.prove(t)
			q := f.tm.GetQuerier(f.ctx)
			target := id.New()
			_, err := q.Exec(f.ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,chain_activation_status) VALUES($1,$1::uuid::text,'Repair target',$2,'0x00000000000000000000000000000000000000cd','target-key','pool','merchant_pool','activating')`, target, f.reservation.NetworkID)
			require.NoError(t, err)
			_, err = q.Exec(f.ctx, `INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status) VALUES($1,$2,$3,'signing')`, f.reservation.BasisID, target, f.reservation.NetworkID)
			require.NoError(t, err)
			engine := posting.NewEngine(nil)
			engine.AddVisitor(&posting.CryptoBalanceVisitor{})
			engine.AddVisitor(&posting.CryptoPlatformCostVisitor{})
			engine.AddRecorder(posting.NewCryptoBalanceRecorder(crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())))
			engine.AddRecorder(posting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(register_repo.NewCryptoPlatformCostRepo())))
			repo := document_repo.NewCryptoNetworkOperationRepo()
			service := crypto_network_operation.NewService(repo, engine, fixedNetworkOperationNumerator{}, nil)
			coordinator := NewCryptoNetworkOperationCoordinator(repo, service, NewFeeBudgetManager())
			outbound := NewOutboundRepo()
			operation, err := coordinator.EnsureActivationBroadcast(f.ctx, AccountActivationTask{ActivationID: f.reservation.BasisID, WalletID: target, NetworkID: f.reservation.NetworkID, Address: "0x00000000000000000000000000000000000000cd", HotWallet: HotWallet{ID: f.reservation.PayerWalletID}, CostTokenID: f.reservation.NativeTokenID}, f.receipt.TxHash)
			require.NoError(t, err)
			_, err = q.Exec(f.ctx, `UPDATE reg_crypto_account_activations SET status='confirmed',cost_status='unknown' WHERE id=$1`, f.reservation.BasisID)
			require.NoError(t, err)
			claim := func() outboundapp.ActivationCostRepairTask {
				tasks, err := outbound.ClaimActivationCostRepairs(f.ctx, "same-worker", 100)
				require.NoError(t, err)
				for _, task := range tasks {
					if task.ActivationID == f.reservation.BasisID {
						return task
					}
				}
				t.Fatal("expected activation cost repair claim")
				return outboundapp.ActivationCostRepairTask{}
			}
			snapshot := func() string {
				var result string
				require.NoError(t, q.QueryRow(f.ctx, `SELECT to_jsonb(a)::text FROM reg_crypto_account_activations a WHERE id=$1`, f.reservation.BasisID).Scan(&result))
				return result
			}
			counts := func() []int {
				var money, cost, binding int
				require.NoError(t, q.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoNetworkOperation'),(SELECT count(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1 AND recorder_type='CryptoNetworkOperation'),(SELECT count(*) FROM reg_crypto_network_fee_bindings WHERE reservation_id=$2 AND recorder_id=$1 AND recorder_type='CryptoNetworkOperation')`, operation, f.reservation.ID).Scan(&money, &cost, &binding))
				return []int{money, cost, binding}
			}
			first := claim()
			require.NoError(t, outbound.PrepareActivationCostRepair(f.ctx, first))
			if authoritative {
				finalized, err := coordinator.FinalizeActivation(f.ctx, first.ActivationID, f.reservation.ID, f.receipt)
				require.NoError(t, err)
				require.True(t, finalized.Posted)
			} else {
				_, err = q.Exec(f.ctx, `UPDATE reg_crypto_account_activations SET cost_lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, first.ActivationID)
				require.NoError(t, err)
				next := claim()
				require.NotEqual(t, first.CostClaimToken, next.CostClaimToken)
				before := snapshot()
				_, err = coordinator.FinalizeActivationRepair(f.ctx, first, f.reservation.ID, f.receipt)
				require.ErrorContains(t, err, "claim is missing or stale")
				altered := next
				altered.TxHash = "another-hash"
				_, err = coordinator.FinalizeActivationRepair(f.ctx, altered, f.reservation.ID, f.receipt)
				require.ErrorContains(t, err, "claim is missing or stale")
				require.Equal(t, before, snapshot())
				require.Equal(t, []int{0, 0, 0}, counts())
				finalized, err := coordinator.FinalizeActivationRepair(f.ctx, next, f.reservation.ID, f.receipt)
				require.NoError(t, err)
				require.True(t, finalized.Posted)
				require.Equal(t, f.receipt.Fee.TotalFee.String(), finalized.CostAmount.String())
			}
			recorded := snapshot()
			require.ErrorContains(t, outbound.MarkActivationCostRepairHeld(f.ctx, first, "late provider response"), "claim is missing or stale")
			_, err = coordinator.FinalizeActivationRepair(f.ctx, first, f.reservation.ID, f.receipt)
			require.ErrorContains(t, err, "claim is missing or stale")
			require.Equal(t, recorded, snapshot())
			require.Equal(t, []int{1, 1, 1}, counts())
			_, err = coordinator.FinalizeActivation(f.ctx, first.ActivationID, f.reservation.ID, f.receipt)
			require.NoError(t, err)
			require.Equal(t, []int{1, 1, 1}, counts())
		})
	}
}

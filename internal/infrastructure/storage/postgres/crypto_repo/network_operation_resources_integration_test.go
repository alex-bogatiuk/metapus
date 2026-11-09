//go:build integration

package crypto_repo_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"

	"github.com/stretchr/testify/require"
)

func networkOperationResourcesFixture(t *testing.T) (crypto_repo.ActivationSelectorFixture, id.ID, id.ID) {
	t.Helper()
	f, activation := activationCostClaimFixture(t)
	var operation id.ID
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT network_operation_id FROM reg_crypto_account_activations WHERE id=$1`, activation).Scan(&operation))
	settlement := id.New()
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_chain_settlements(id,network_id,basis_type,basis_id,tx_hash,status,block_hash,block_height,finality_status,native_token_id,total_fee_minor,raw_receipt,observed_at)
 VALUES($1,$2,'CryptoNetworkOperation',$3,$3::uuid::text,'succeeded',repeat('a',64),10,'solidified',$4,5,'{}',NOW())`, settlement, f.Network, operation, f.Native)
	require.NoError(t, err)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE doc_crypto_network_operations SET status='succeeded',chain_settlement_id=$2,finalized_at=NOW(),cost_amount=5 WHERE id=$1`, operation, settlement)
	require.NoError(t, err)
	return f, activation, operation
}

func networkOperationService() *crypto_network_operation.Service {
	engine := posting.NewEngine(postgres.NewDocLocker())
	engine.AddVisitor(&posting.CryptoBalanceVisitor{})
	engine.AddVisitor(&posting.CryptoPlatformCostVisitor{})
	engine.AddRecorder(posting.NewCryptoBalanceRecorder(crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())))
	engine.AddRecorder(posting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(register_repo.NewCryptoPlatformCostRepo())))
	return crypto_network_operation.NewService(document_repo.NewCryptoNetworkOperationRepo(), engine, executionNumerator{}, nil)
}

func TestNetworkOperationProtectionAndRowsFollowResourcePrefix(t *testing.T) {
	for _, command := range []string{"protection", "update", "for_update", "post", "unpost", "repost", "deletion_mark"} {
		t.Run(command, func(t *testing.T) {
			f, _, operation := networkOperationResourcesFixture(t)
			repo := document_repo.NewCryptoNetworkOperationRepo()
			service := networkOperationService()
			if command == "unpost" || command == "repost" || command == "deletion_mark" {
				require.NoError(t, service.Post(f.Ctx, operation))
			}
			f.AssertLockOrder(t, func(ctx context.Context) error {
				_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.Hot)
				return err
			}, func(ctx context.Context) error {
				_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT id FROM doc_crypto_network_operations WHERE id=$1 FOR UPDATE`, operation)
				return err
			}, func(ctx context.Context) error {
				switch command {
				case "protection":
					return repo.SetManualEditAllowed(ctx, operation, true)
				case "for_update":
					_, err := repo.GetByIDForUpdate(ctx, operation)
					return err
				case "post":
					return service.Post(ctx, operation)
				case "unpost":
					return service.Unpost(ctx, operation)
				case "deletion_mark":
					return service.SetDeletionMark(ctx, operation, true)
				default:
					doc, err := repo.GetByID(ctx, operation)
					if err != nil {
						return err
					}
					doc.Description = "metadata correction"
					if command == "repost" {
						return service.UpdateAndRepost(ctx, doc)
					}
					return repo.Update(ctx, doc)
				}
			})
		})
	}
}

func TestNetworkOperationWorkflowAdvisoryPrecedesOperation(t *testing.T) {
	f, activation, operation := networkOperationResourcesFixture(t)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		return postgres.NewDocLocker().LockDocument(ctx, crypto_network_operation.ActivationTaskBasisType, activation)
	}, func(ctx context.Context) error {
		return postgres.NewDocLocker().LockDocument(ctx, crypto_network_operation.DocumentType, operation)
	}, func(ctx context.Context) error {
		return networkOperationService().Post(ctx, operation)
	})
}

func TestNetworkOperationPolicyPrecedesWalletAndHistoricalFootprint(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{false: "pool_policy", true: "historical_wallet"}[historical], func(t *testing.T) {
			f, _, operation := networkOperationResourcesFixture(t)
			wallet := f.Hot
			if historical {
				wallet = id.New()
				_, err := f.Pool.Exec(f.Ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Previous payer',$2,$1::uuid::text,'history-key')`, wallet, f.Network)
				require.NoError(t, err)
				_, err = f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,100)`, wallet, f.Native)
				require.NoError(t, err)
				_, err = f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,$2,'CryptoNetworkOperation',1,NOW(),'expense',$3,$4,1)`, id.New(), operation, wallet, f.Native)
				require.NoError(t, err)
			}
			f.AssertLockOrder(t, func(ctx context.Context) error {
				q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
				if historical {
					_, err := q.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, wallet)
					return err
				}
				_, err := q.Exec(ctx, `SELECT network_id FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 FOR UPDATE`, f.Network)
				return err
			}, func(ctx context.Context) error {
				q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
				if historical {
					_, err := q.Exec(ctx, `SELECT id FROM doc_crypto_network_operations WHERE id=$1 FOR UPDATE`, operation)
					return err
				}
				_, err := q.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.Hot)
				return err
			}, func(ctx context.Context) error {
				return document_repo.NewCryptoNetworkOperationRepo().SetManualEditAllowed(ctx, operation, true)
			})
		})
	}
}

func TestNetworkOperationProtectionReplayPreservesActorAndFinancialState(t *testing.T) {
	f, activation, operation := networkOperationResourcesFixture(t)
	repo := document_repo.NewCryptoNetworkOperationRepo()
	before := activationSnapshot(t, f, activation)
	require.NoError(t, repo.SetManualEditAllowed(f.Ctx, operation, true))
	var first, replay string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(d)::text FROM doc_crypto_network_operations d WHERE id=$1`, operation).Scan(&first))
	require.NoError(t, repo.SetManualEditAllowed(f.Ctx, operation, true))
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(d)::text FROM doc_crypto_network_operations d WHERE id=$1`, operation).Scan(&replay))
	require.Equal(t, first, replay)
	require.Equal(t, before, activationSnapshot(t, f, activation))
	tm := postgres.MustGetTxManager(f.Ctx)
	require.NoError(t, tm.RunInTransactionWithOptions(f.Ctx, postgres.SerializableTxOptions(), func(ctx context.Context) error {
		require.ErrorContains(t, repo.SetManualEditAllowed(ctx, operation, false), "read committed")
		return nil
	}))
	_, err := repo.GetByIDForUpdate(f.Ctx, operation)
	require.ErrorContains(t, err, "require a transaction")
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT to_jsonb(d)::text FROM doc_crypto_network_operations d WHERE id=$1`, operation).Scan(&replay))
	require.Equal(t, first, replay)
}

func TestNetworkOperationRejectsChangedFootprintAfterPolicyWait(t *testing.T) {
	f, _, operation := networkOperationResourcesFixture(t)
	wallet := id.New()
	_, err := f.Pool.Exec(f.Ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id) VALUES($1,$1::uuid::text,'Changed payer',$2,$1::uuid::text,'changed-key')`, wallet, f.Network)
	require.NoError(t, err)
	f.AssertLockOrder(t, func(ctx context.Context) error {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `SELECT network_id FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 FOR UPDATE`, f.Network)
		return err
	}, func(ctx context.Context) error {
		// Simulate a legacy writer changing the footprint while this actor waits.
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_crypto_network_operations SET source_wallet_id=$2 WHERE id=$1`, operation, wallet)
		return err
	}, func(ctx context.Context) error {
		err := document_repo.NewCryptoNetworkOperationRepo().SetManualEditAllowed(ctx, operation, true)
		if err == nil || !strings.Contains(err.Error(), "footprint changed") {
			return fmt.Errorf("changed footprint must reject the metadata write: %v", err)
		}
		return nil
	})
	doc, err := document_repo.NewCryptoNetworkOperationRepo().GetByID(f.Ctx, operation)
	require.NoError(t, err)
	require.Equal(t, wallet, doc.SourceWalletID)
	require.False(t, doc.ManualEditAllowed)
}

func TestNetworkOperationManualRepostKeepsSingleNetFee(t *testing.T) {
	f, _, operation := networkOperationResourcesFixture(t)
	service := networkOperationService()
	require.NoError(t, service.Post(f.Ctx, operation))
	admin := appctx.WithUser(f.Ctx, &appctx.UserContext{Roles: []string{"admin"}})
	doc, err := service.AllowEditing(admin, operation)
	require.NoError(t, err)
	doc.Description = "audited correction"
	require.NoError(t, service.UserService().UpdateAndRepost(admin, doc))
	var net string
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT COALESCE(sum(CASE WHEN record_type='expense' THEN amount ELSE -amount END),0)::text FROM reg_crypto_balance_movements WHERE recorder_id=$1`, operation).Scan(&net))
	require.Equal(t, "5", net)
	current, err := document_repo.NewCryptoNetworkOperationRepo().GetByID(f.Ctx, operation)
	require.NoError(t, err)
	require.False(t, current.ManualEditAllowed)
	// Repost appends a reversal generation and then the replacement posting.
	require.Equal(t, 3, current.PostedVersion)
	ctx, cancel := context.WithTimeout(f.Ctx, time.Second)
	defer cancel()
	require.ErrorContains(t, service.UserService().Unpost(appctx.WithUser(ctx, &appctx.UserContext{Roles: []string{"admin"}}), operation), "strict document is locked")
}

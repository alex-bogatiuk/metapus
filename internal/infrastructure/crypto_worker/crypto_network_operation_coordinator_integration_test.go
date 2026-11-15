//go:build integration

package crypto_worker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/numerator"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

type fixedNetworkOperationNumerator struct{}

func (fixedNetworkOperationNumerator) GetNextNumber(context.Context, numerator.Config, *numerator.Options, time.Time) (string, error) {
	return "CNO-TEST", nil
}
func (fixedNetworkOperationNumerator) SetNextNumber(context.Context, numerator.Config, time.Time, int64) error {
	return nil
}

func TestIntegrationActivationFinalityPostsDocumentOnce(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	for _, receiptStatus := range []crypto.ReceiptStatus{crypto.ReceiptSucceeded, crypto.ReceiptReverted} {
		t.Run(string(receiptStatus), func(t *testing.T) {
			baseCtx := context.Background()
			pool, err := pgxpool.New(baseCtx, dsn)
			require.NoError(t, err)
			defer pool.Close()
			ctx := tenant.WithTxManager(baseCtx, postgres.NewTxManagerFromRawPool(pool))

			networkID, tokenID := id.New(), id.New()
			sourceWalletID, targetWalletID, activationID, reservationID := id.New(), id.New(), id.New(), id.New()
			suffix := networkID.String()
			txHash := strings.Repeat("ac", 16) + strings.ReplaceAll(suffix, "-", "")
			_, err = pool.Exec(ctx, `
				INSERT INTO cat_blockchain_networks
				(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals)
				VALUES ($1,$2,'CNO test','tron',$3,'TRX',6)
			`, networkID, "CNO-NET-"+suffix, "cno-"+suffix)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `
				INSERT INTO cat_tokens
				(id,code,name,network_id,symbol,decimal_places,token_standard,contract_address)
				VALUES ($1,$2,'TRX',$3,'TRX',6,'native','')
			`, tokenID, "CNO-TRX-"+suffix, networkID)
			require.NoError(t, err)
			sourceAddress := testTRONAddress()
			_, err = pool.Exec(ctx, `
				INSERT INTO cat_wallets
				(id,code,name,network_id,address,tier,status,vault_key_id,chain_activation_status,wallet_role)
				VALUES
				($1,$2,'Source',$3,$4,'hot','free','vault/source','active','liquidity_hot'),
				($5,$6,'Target',$3,$7,'pool','free','vault/target','activating','merchant_pool')
			`, sourceWalletID, "CNO-SOURCE-"+suffix, networkID, sourceAddress,
				targetWalletID, "CNO-TARGET-"+suffix, "TTarget"+suffix)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO reg_crypto_balance_balances(wallet_id,token_id,amount) VALUES($1,$2,1000)`, sourceWalletID, tokenID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `
				INSERT INTO reg_crypto_account_activations(id,wallet_id,network_id,status)
				VALUES($1,$2,$3,'signing')
			`, activationID, targetWalletID, networkID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `
				INSERT INTO reg_crypto_fee_reservations(
					id,network_id,payer_wallet_id,native_token_id,operation_token_id,
					basis_type,basis_id,purpose,attempt,strategy,max_fee_minor,tx_hash,status
				) VALUES($1,$2,$3,$4,$4,$5,$6,'activation',1,'native_burn',100,$7,'broadcast')
			`, reservationID, networkID, sourceWalletID, tokenID,
				crypto_network_operation.ActivationTaskBasisType, activationID, txHash)
			require.NoError(t, err)

			engine := posting.NewEngine(nil)
			engine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
			engine.AddVisitor(&cryptoposting.CryptoPlatformCostVisitor{})
			engine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())))
			platformRepo := register_repo.NewCryptoPlatformCostRepo()
			engine.AddRecorder(cryptoposting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(platformRepo)))
			repo := document_repo.NewCryptoNetworkOperationRepo()
			service := crypto_network_operation.NewService(repo, engine, fixedNetworkOperationNumerator{}, nil)
			feeBudget := NewFeeBudgetManager()
			coordinator := NewCryptoNetworkOperationCoordinator(repo, service, feeBudget)
			task := AccountActivationTask{
				ActivationID: activationID, WalletID: targetWalletID, NetworkID: networkID,
				Address: "TTarget" + suffix, HotWallet: HotWallet{ID: sourceWalletID}, CostTokenID: tokenID,
			}
			var documentsBeforeBroadcast int
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM doc_crypto_network_operations WHERE basis_id=$1`, activationID).Scan(&documentsBeforeBroadcast))
			require.Zero(t, documentsBeforeBroadcast)

			operationID, err := coordinator.EnsureActivationBroadcast(ctx, task, txHash)
			require.NoError(t, err)
			repeatedID, err := coordinator.EnsureActivationBroadcast(ctx, task, txHash)
			require.NoError(t, err)
			require.Equal(t, operationID, repeatedID)

			receipt := crypto.ChainReceipt{
				NetworkID: networkID, TxHash: txHash, Status: receiptStatus,
				BlockHash: strings.Repeat("b", 64), BlockHeight: 10, Confirmations: 20,
				Canonicality: crypto.CanonicalityCanonical, Finality: crypto.FinalitySolidified,
				Fee: crypto.FeeSettlementReport{
					NativeAsset: crypto.AssetRef{TokenID: tokenID}, TotalFee: types.NewCryptoAmountFromInt64(25),
					Components: []crypto.FeeComponent{{Name: "energy", Amount: types.NewCryptoAmountFromInt64(25)}},
				},
				RawReceipt: json.RawMessage(`{"result":true}`), ObservedAt: time.Now().UTC(),
			}
			receipt = reproveNetworkFee(t, ctx, receipt)
			require.ErrorContains(t, feeBudget.Settle(ctx, reservationID, receipt), "must be settled through CryptoNetworkOperation")
			finalized, err := coordinator.FinalizeActivation(ctx, activationID, reservationID, receipt)
			require.NoError(t, err)
			require.True(t, finalized.Posted)
			require.True(t, types.NewCryptoAmountFromInt64(25).Equal(finalized.CostAmount))
			// Cost repair must replay the original owner and fee without
			// introducing another posting, for success and revert alike.
			_, err = pool.Exec(ctx, `UPDATE reg_crypto_account_activations SET cost_status='unknown' WHERE id=$1`, activationID)
			require.NoError(t, err)
			repairs, err := NewOutboundRepo().ClaimActivationCostRepairs(ctx, "cost-repair", 100)
			require.NoError(t, err)
			var repaired bool
			for _, repair := range repairs {
				if repair.ActivationID == activationID {
					_, err = coordinator.FinalizeActivationRepair(ctx, repair, reservationID, receipt)
					require.NoError(t, err)
					repaired = true
				}
			}
			require.True(t, repaired)
			_, err = coordinator.FinalizeActivation(ctx, activationID, reservationID, receipt)
			require.NoError(t, err)

			var docs, balanceMovements, platformMovements int
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM doc_crypto_network_operations WHERE basis_id=$1`, activationID).Scan(&docs))
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoNetworkOperation'`, operationID).Scan(&balanceMovements))
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1 AND recorder_type='CryptoNetworkOperation'`, operationID).Scan(&platformMovements))
			require.Equal(t, 1, docs)
			require.Equal(t, 1, balanceMovements)
			require.Equal(t, 1, platformMovements)
			// Reposting may create a new version, but duplicate effects within
			// one version must still be rejected by the database.
			movements, err := platformRepo.GetMovementsByRecorder(ctx, operationID)
			require.NoError(t, err)
			require.Len(t, movements, 1)
			movements[0].LineID = id.New()
			err = platformRepo.CreateMovements(ctx, movements)
			var duplicate *pgconn.PgError
			require.ErrorAs(t, err, &duplicate)
			require.Equal(t, "23505", duplicate.Code)
			require.Equal(t, "idx_crypto_platform_cost_effect", duplicate.ConstraintName)

			assertAccounting := func(wantMovements int, wantCost int64) {
				t.Helper()
				for _, table := range []string{"reg_crypto_balance_movements", "reg_crypto_platform_cost_movements"} {
					var count, reversals int
					var cost int64
					require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(`
						SELECT COUNT(*),COUNT(reversal_of_line_id),
						       COALESCE(SUM(CASE WHEN record_type='expense' THEN amount ELSE -amount END),0)
						FROM %s WHERE recorder_id=$1`, table), operationID).Scan(&count, &reversals, &cost))
					require.Equal(t, wantMovements, count, table)
					require.Equal(t, wantMovements/2, reversals, table)
					require.Equal(t, wantCost, cost, table)
					var linkedReversals int
					require.NoError(t, pool.QueryRow(ctx, fmt.Sprintf(`
						SELECT COUNT(*) FROM %[1]s reversal
						JOIN %[1]s original ON original.line_id=reversal.reversal_of_line_id
						WHERE reversal.recorder_id=$1 AND original.recorder_id=$1
						  AND reversal.recorder_type='CryptoLedgerReversal'
						  AND reversal.record_type<>original.record_type
						  AND reversal.amount=original.amount`, table), operationID).Scan(&linkedReversals))
					require.Equal(t, reversals, linkedReversals, table)
				}
			}
			assertAccounting(1, 25)

			var operationStatus, taskStatus, walletStatus, costType, purpose, provider, externalOrderID string
			require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM doc_crypto_network_operations WHERE id=$1`, operationID).Scan(&operationStatus))
			require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM reg_crypto_account_activations WHERE id=$1`, activationID).Scan(&taskStatus))
			require.NoError(t, pool.QueryRow(ctx, `SELECT chain_activation_status FROM cat_wallets WHERE id=$1`, targetWalletID).Scan(&walletStatus))
			require.NoError(t, pool.QueryRow(ctx, `SELECT cost_type,purpose,provider,external_order_id FROM reg_crypto_platform_cost_movements WHERE recorder_id=$1`, operationID).Scan(&costType, &purpose, &provider, &externalOrderID))
			require.Equal(t, "chain_network_fee", costType)
			require.Equal(t, "activation", purpose)
			require.Equal(t, "chain", provider)
			require.Equal(t, txHash, externalOrderID)
			var basisType string
			require.NoError(t, pool.QueryRow(ctx, `SELECT basis_type FROM doc_crypto_network_operations WHERE id=$1`, operationID).Scan(&basisType))
			require.Equal(t, crypto_network_operation.ActivationTaskBasisType, basisType)
			if receiptStatus == crypto.ReceiptReverted {
				require.Equal(t, "reverted", operationStatus)
				require.Equal(t, "failed", taskStatus)
				require.Equal(t, "failed", walletStatus)
			} else {
				require.Equal(t, "succeeded", operationStatus)
				require.Equal(t, "confirmed", taskStatus)
				require.Equal(t, "activating", walletStatus)

				// A recognized network fee is frozen: unpost cannot rewrite it.
				require.ErrorContains(t, service.Unpost(ctx, operationID), "explicit compensation workflow")
				assertAccounting(1, 25)
			}
		})
	}
}

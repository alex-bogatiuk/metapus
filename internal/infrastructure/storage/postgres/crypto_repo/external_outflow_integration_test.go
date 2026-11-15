//go:build integration

package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/registers/crypto_balance"
	"metapus/internal/domain/registers/crypto_platform_cost"
	"metapus/internal/domain/registers/cryptoposting"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

func TestExternalOutflowCreatesOneIncidentAndFailClosedHolds(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
	if dsn == "" {
		t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	txm := postgres.NewTxManagerFromRawPool(pool)
	ctx = tenant.WithTxManager(ctx, txm)

	errRollback := errors.New("rollback external outflow fixture")
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		networkID, tokenID, walletID := id.New(), id.New(), id.New()
		suffix := id.Compact(networkID)
		fromAddress := "TExternalOutflowSource" + suffix
		contractAddress := "TExternalOutflowToken" + suffix
		_, err := q.Exec(txCtx, `
			INSERT INTO cat_blockchain_networks(
				id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
			VALUES($1,$2,'External outflow test','tron',$3,'TRX',6,FALSE);
			INSERT INTO cat_tokens(
				id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
			VALUES($4,$5,'External outflow token',$1,$6,$7,6,'TRC-20');
			INSERT INTO cat_wallets(
				id,code,name,network_id,address,tier,wallet_role,status,is_active,chain_activation_status)
			VALUES($8,$9,'External outflow source',$1,$10,'pool','merchant_pool','free',TRUE,'active')
		`, pgx.QueryExecModeSimpleProtocol, networkID, "OUTFLOW-NET-"+suffix, "outflow-"+suffix, tokenID,
			"OUTFLOW-TOKEN-"+suffix, contractAddress, "OF"+suffix[:6], walletID,
			"OUTFLOW-WALLET-"+suffix, fromAddress)
		if err != nil {
			return err
		}

		event := crypto.BlockchainEvent{
			NetworkID:     networkID,
			TxHash:        "outflow-tx-" + suffix,
			FromAddress:   fromAddress,
			ToAddress:     "TExternalDestination" + suffix,
			TokenContract: contractAddress,
			Amount:        types.MustCryptoAmount("42000000"),
			BlockNumber:   100,
			Confirmations: 1,
			RequiredConfs: 19,
			Direction:     crypto.TransferDirectionOutgoing,
		}
		recorder := newIntegrationExternalOutflowRecorder(txm)
		if err := recorder.RecordExternalOutflow(txCtx, event); err != nil {
			return err
		}

		var outflows, incidents, observations, outflowObservations, networkHolds, walletHolds int
		if err := q.QueryRow(txCtx, `
			SELECT
				(SELECT COUNT(*) FROM reg_crypto_external_outflows WHERE network_id=$1),
				(SELECT COUNT(*) FROM reg_crypto_reconciliation_items WHERE network_id=$1 AND kind='external_outflow'),
				(SELECT COUNT(*) FROM reg_crypto_reconciliation_observations WHERE network_id=$1 AND kind='external_outflow'),
				(SELECT COUNT(*) FROM reg_crypto_external_outflow_observations WHERE network_id=$1),
				(SELECT COUNT(*) FROM reg_crypto_network_holds WHERE network_id=$1 AND reason='external_outflow' AND status='open'),
				(SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$2 AND reason='external_outflow' AND status='open')
		`, networkID, walletID).Scan(&outflows, &incidents, &observations, &outflowObservations, &networkHolds, &walletHolds); err != nil {
			return err
		}
		require.Equal(t, 1, outflows)
		require.Equal(t, 1, incidents)
		require.Equal(t, 1, observations)
		require.Equal(t, 1, outflowObservations)
		require.Equal(t, 1, networkHolds)
		require.Equal(t, 1, walletHolds)

		event.BlockNumber = 101
		event.Confirmations = 19
		if err := recorder.RecordExternalOutflow(txCtx, event); err != nil {
			return err
		}

		var confirmations int
		if err := q.QueryRow(txCtx, `
			SELECT
				(SELECT COUNT(*) FROM reg_crypto_reconciliation_items WHERE network_id=$1 AND kind='external_outflow'),
				(SELECT COUNT(*) FROM reg_crypto_reconciliation_observations WHERE network_id=$1 AND kind='external_outflow'),
				(SELECT COUNT(*) FROM reg_crypto_external_outflow_observations WHERE network_id=$1),
				(SELECT confirmations FROM reg_crypto_external_outflows WHERE network_id=$1 AND tx_hash=$2)
		`, networkID, event.TxHash).Scan(&incidents, &observations, &outflowObservations, &confirmations); err != nil {
			return err
		}
		require.Equal(t, 1, incidents)
		require.Equal(t, 2, observations)
		require.Equal(t, 2, outflowObservations)
		require.Equal(t, 19, confirmations)

		event.Amount = types.MustCryptoAmount("42000001")
		conflictErr := recorder.RecordExternalOutflow(txCtx, event)
		var appErr *apperror.AppError
		require.ErrorAs(t, conflictErr, &appErr)
		require.Equal(t, apperror.CodeConflict, appErr.Code)
		return errRollback
	})
	require.ErrorIs(t, err, errRollback)
}

func TestConcurrentExternalOutflowWorkflowCreatesOneAdjustmentAndSelectiveResolution(t *testing.T) {
	for _, internalV2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("internal_v2_%t", internalV2), func(t *testing.T) {
			dsn := os.Getenv("METAPUS_TEST_CRYPTO_DSN")
			if dsn == "" {
				t.Fatal("METAPUS_TEST_CRYPTO_DSN is required for integration tests")
			}

			ctx := context.Background()
			pool, err := pgxpool.New(ctx, dsn)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			txm := postgres.NewTxManagerFromRawPool(pool)
			ctx = tenant.WithTxManager(ctx, txm)

			networkID, tokenID, walletID := id.New(), id.New(), id.New()
			suffix := id.Compact(networkID)
			fromAddress := "TConcurrentOutflowSource" + suffix
			contractAddress := "TConcurrentOutflowToken" + suffix
			standard := "TRC-20"
			if internalV2 {
				contractAddress, standard = "", "native"
			}
			event := crypto.BlockchainEvent{
				NetworkID: networkID, TxHash: id.Compact(networkID) + id.Compact(networkID),
				EventKind: crypto.EventKindContractLog, EventIndex: 7,
				FromAddress: fromAddress, ToAddress: "TConcurrentOutflowDestination" + suffix,
				TokenContract: contractAddress, Amount: types.MustCryptoAmount("42000000"),
				BlockNumber: 100, BlockHash: strings.Repeat("b", 64),
				Confirmations: 20, RequiredConfs: 20, Finality: crypto.FinalityFinalized,
				Direction: crypto.TransferDirectionOutgoing,
			}
			if internalV2 {
				event.EventKind, event.DecoderVersion, event.TraceHash = crypto.EventKindInternalValue, 2, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				index, rawIndex := int64(0), int64(9)
				event.CallValueIndex, event.TraceIndex = &index, &rawIndex
			}
			require.NoError(t, txm.RunInTransaction(ctx, func(txCtx context.Context) error {
				q := txm.GetQuerier(txCtx)
				if _, execErr := q.Exec(txCtx, `
			INSERT INTO cat_blockchain_networks(
				id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
			VALUES($1,$2,'Concurrent external outflow test','tron',$3,'TRX',6,FALSE);
			INSERT INTO cat_tokens(
				id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
			VALUES($4,$5,'Concurrent external outflow token',$1,$6,$7,6,$11);
			INSERT INTO cat_wallets(
				id,code,name,network_id,address,tier,wallet_role,status,is_active,chain_activation_status,vault_key_id)
			VALUES($8,$9,'Concurrent external outflow source',$1,$10,'pool','merchant_pool','free',TRUE,'active','test-key')
		`, pgx.QueryExecModeSimpleProtocol, networkID, "OFR-NET-"+suffix, "outflow-race-"+suffix, tokenID,
					"OFR-TOKEN-"+suffix, contractAddress, "OR"+suffix[:6], walletID,
					"OFR-WALLET-"+suffix, fromAddress, standard); execErr != nil {
					return execErr
				}
				if _, execErr := q.Exec(txCtx, `
			INSERT INTO reg_crypto_balance_movements(
				line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$2,'IntegrationCustodySeed',1,NOW(),'receipt',$3,$4,$5)
		`, id.New(), id.New(), walletID, tokenID, event.Amount); execErr != nil {
					return execErr
				}
				return newIntegrationExternalOutflowRecorder(txm).RecordExternalOutflow(txCtx, event)
			}))

			if internalV2 {
				rawIndex := int64(8)
				event.TraceIndex = &rawIndex
				require.NoError(t, txm.RunInTransaction(ctx, func(ctx context.Context) error {
					return newIntegrationExternalOutflowRecorder(txm).RecordExternalOutflow(ctx, event)
				}))
			}
			proof := crypto.TransferVerification{Event: event, DecoderVersion: 2, MatchingEffects: 1, ExecutionStatus: crypto.ReceiptSucceeded, Outcome: "verified", VerifiedAt: time.Now().UTC(), Sources: []crypto.VerificationSource{
				{ProviderID: "one", TrustGroup: "one", LatestProviderID: "one", Digest: "canonical", SourceDigest: "fixture", DecoderVersion: 2, HeadHeight: 119, HeadHash: strings.Repeat("c", 64)},
				{ProviderID: "two", TrustGroup: "two", LatestProviderID: "two", Digest: "canonical", SourceDigest: "fixture", DecoderVersion: 2, HeadHeight: 119, HeadHash: strings.Repeat("c", 64)},
			}}
			proof.Event.DecoderVersion = 2
			proof.Event.Canonicality = crypto.CanonicalityCanonical
			proof.Event.Finality = crypto.FinalitySolidified
			require.NoError(t, proof.BindTRONAccountingIdentity())
			_, err = pool.Exec(ctx, `INSERT INTO sys_crypto_block_ingestion(network_id,start_height,start_hash,start_timestamp,mode,decoder_version) VALUES($1,0,'fixture',0,'paused',2)`, networkID)
			require.NoError(t, err)
			_, err = NewBlockIngestionRepo().SaveVerification(ctx, proof)
			require.NoError(t, err)
			var outflowID id.ID
			require.NoError(t, pool.QueryRow(ctx, `
		SELECT id FROM reg_crypto_external_outflows
		WHERE network_id=$1 AND tx_hash=$2 AND event_kind=$3 AND event_index=$4
	`, networkID, event.TxHash, event.EventKind, event.EventIndex).Scan(&outflowID))

			workflowRepo := NewExternalOutflowWorkflowRepo()
			holds := cryptoapp.NewOperationalHoldService(txm, NewOperationalHoldRepo())
			postingEngine := posting.NewEngine(postgres.NewDocLocker())
			postingEngine.AddVisitor(&cryptoposting.CryptoBalanceVisitor{})
			postingEngine.AddVisitor(&cryptoposting.CryptoPlatformCostVisitor{})
			postingEngine.AddRecorder(cryptoposting.NewCryptoBalanceRecorder(crypto_balance.NewService(register_repo.NewCryptoBalanceRepo())))
			postingEngine.AddRecorder(cryptoposting.NewCryptoPlatformCostRecorder(crypto_platform_cost.NewService(register_repo.NewCryptoPlatformCostRepo())))
			service := cryptoapp.NewExternalOutflowService(txm, workflowRepo, holds, postingEngine)
			classifierA, classifierB := id.New(), id.New()
			classificationErrors := runConcurrentOutflowCommands([]id.ID{classifierA, classifierB}, func(actorID id.ID) error {
				_, classifyErr := service.Classify(ctx, cryptoapp.ClassifyExternalOutflowCommand{
					OutflowID: outflowID, Classification: crypto.OutflowUnauthorizedLoss,
					Reason: "confirmed custody loss",
				}, actorID)
				return classifyErr
			})
			requireOneSuccessOneConflict(t, classificationErrors)

			outflow, err := service.Get(ctx, outflowID)
			require.NoError(t, err)
			require.NotNil(t, outflow.Adjustment)
			adjustmentID := outflow.Adjustment.ID
			classifierID := *outflow.ClassifiedBy

			_, err = pool.Exec(ctx, `
		UPDATE reg_crypto_external_outflows
		SET finality_status='reorged',updated_at=NOW()
		WHERE id=$1
	`, outflowID)
			require.NoError(t, err)
			_, err = service.ApproveAdjustment(ctx, adjustmentID, id.New(), "must not post a reorged loss")
			var appErr *apperror.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, apperror.CodeConflict, appErr.Code)
			var prematureWalletMovements, prematurePlatformMovements int
			require.NoError(t, pool.QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM reg_crypto_balance_movements
		   WHERE recorder_type='CryptoCustodyAdjustment' AND recorder_id=$1),
		  (SELECT COUNT(*) FROM reg_crypto_platform_cost_movements
		   WHERE recorder_type='CryptoCustodyAdjustment' AND recorder_id=$1)
	`, adjustmentID).Scan(&prematureWalletMovements, &prematurePlatformMovements))
			require.Zero(t, prematureWalletMovements)
			require.Zero(t, prematurePlatformMovements)
			_, err = pool.Exec(ctx, `
		UPDATE reg_crypto_external_outflows
		SET finality_status='finalized',updated_at=NOW()
		WHERE id=$1
	`, outflowID)
			require.NoError(t, err)
			var prePostingRunID id.ID
			require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO reg_crypto_reconciliation_runs(
			network_id,status,worker_id,started_at,completed_at,wallets_checked,items_created)
		VALUES($1,'completed','integration-pre-posting',clock_timestamp(),clock_timestamp(),1,0)
		RETURNING id
	`, networkID).Scan(&prePostingRunID))
			_, err = pool.Exec(ctx, `
		INSERT INTO reg_crypto_reconciliation_checks(
			run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount)
		VALUES($1,$2,$3,$4,0,0,0)
	`, prePostingRunID, networkID, walletID, tokenID)
			require.NoError(t, err)

			checkerA, checkerB := id.New(), id.New()
			_, err = pool.Exec(ctx, `
		UPDATE doc_crypto_custody_adjustments
		SET status='posted',decision_by=$2,decision_reason='direct SQL bypass',
				    posted_version=1,decided_at=NOW(),decision_basis=jsonb_build_object('id',$3::uuid)
		WHERE id=$1
		`, adjustmentID, checkerA, id.New())
			require.Error(t, err)
			require.Contains(t, err.Error(), "posted custody adjustment requires exactly one wallet expense")

			approvalErrors := runConcurrentOutflowCommands([]id.ID{checkerA, checkerB}, func(actorID id.ID) error {
				_, approveErr := service.ApproveAdjustment(ctx, adjustmentID, actorID, "custody accounting approved")
				return approveErr
			})
			requireOneSuccessOneConflict(t, approvalErrors)

			var adjustmentCount, walletMovements, platformMovements, merchantMovements int
			require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM doc_crypto_custody_adjustments WHERE external_outflow_id=$1),
			(SELECT COUNT(*) FROM reg_crypto_balance_movements WHERE recorder_type='CryptoCustodyAdjustment' AND recorder_id=$2),
			(SELECT COUNT(*) FROM reg_crypto_platform_cost_movements WHERE recorder_type='CryptoCustodyAdjustment' AND recorder_id=$2),
			(SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_type='CryptoCustodyAdjustment' AND recorder_id=$2)
	`, outflowID, adjustmentID).Scan(&adjustmentCount, &walletMovements, &platformMovements, &merchantMovements))
			require.Equal(t, 1, adjustmentCount)
			require.Equal(t, 1, walletMovements)
			require.Equal(t, 1, platformMovements)
			require.Zero(t, merchantMovements)
			_, err = pool.Exec(ctx, `
		DELETE FROM reg_crypto_balance_movements
		WHERE recorder_type='CryptoCustodyAdjustment' AND recorder_id=$1
	`, adjustmentID)
			require.Error(t, err)
			require.Contains(t, err.Error(), "custody adjustment wallet movements are immutable")
			_, err = pool.Exec(ctx, `
		DELETE FROM reg_crypto_platform_cost_movements
		WHERE recorder_type='CryptoCustodyAdjustment' AND recorder_id=$1
	`, adjustmentID)
			require.Error(t, err)
			require.Contains(t, err.Error(), "custody adjustment platform movements are immutable")

			prematureResolver := id.New()
			if prematureResolver == classifierID {
				prematureResolver = id.New()
			}
			_, err = service.Resolve(ctx, outflowID, prematureResolver, "pre-posting reconciliation must not qualify")
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, apperror.CodeConflict, appErr.Code)

			otherOwner := id.New().String()
			require.NoError(t, holds.Acquire(ctx, cryptoapp.AcquireOperationalHoldCommand{
				Scope: cryptoapp.OperationalHoldScopeNetwork, ScopeID: networkID, Reason: "deep_reorg",
				OwnerType: "security_incident", OwnerID: otherOwner, HeldBy: "integration-test",
			}))
			require.NoError(t, holds.Acquire(ctx, cryptoapp.AcquireOperationalHoldCommand{
				Scope: cryptoapp.OperationalHoldScopeWallet, ScopeID: walletID, Reason: "deep_reorg",
				OwnerType: "security_incident", OwnerID: otherOwner, HeldBy: "integration-test",
			}))

			var runID id.ID
			require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO reg_crypto_reconciliation_runs(
			network_id,status,worker_id,started_at,completed_at,wallets_checked,items_created)
		VALUES($1,'completed','integration-test',clock_timestamp(),clock_timestamp()+interval '1 second',1,0)
		RETURNING id
	`, networkID).Scan(&runID))
			_, err = pool.Exec(ctx, `
		INSERT INTO reg_crypto_reconciliation_checks(
			run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount,observed_at)
		VALUES($1,$2,$3,$4,0,0,0,clock_timestamp()+interval '1 second')
	`, runID, networkID, walletID, tokenID)
			require.NoError(t, err)

			directResolver := id.New()
			if directResolver == classifierID {
				directResolver = id.New()
			}
			_, err = pool.Exec(ctx, `
		UPDATE reg_crypto_external_outflows
			SET status='resolved',resolved_by=$2,resolved_at=NOW(),resolution='direct SQL bypass',resolution_basis=jsonb_build_object('id',$3::uuid),updated_at=NOW()
		WHERE id=$1 AND status='classified'
		`, outflowID, directResolver, id.New())
			require.Error(t, err)
			require.Contains(t, err.Error(), "resolved external outflow requires its exact resolved reconciliation incident")

			resolverA, resolverB := id.New(), id.New()
			if resolverA == classifierID {
				resolverA = id.New()
			}
			if resolverB == classifierID {
				resolverB = id.New()
			}
			resolutionErrors := runConcurrentOutflowCommands([]id.ID{resolverA, resolverB}, func(actorID id.ID) error {
				_, resolveErr := service.Resolve(ctx, outflowID, actorID, "fresh reconciliation proves zero delta")
				return resolveErr
			})
			requireOneSuccessOneConflict(t, resolutionErrors)

			var externalNetworkOpen, externalWalletOpen, independentNetworkOpen, independentWalletOpen int
			require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM reg_crypto_network_holds WHERE owner_type='external_outflow' AND owner_id=$1 AND status='open'),
			(SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE owner_type='external_outflow' AND owner_id=$1 AND status='open'),
			(SELECT COUNT(*) FROM reg_crypto_network_holds WHERE owner_type='security_incident' AND owner_id=$2 AND status='open'),
			(SELECT COUNT(*) FROM reg_crypto_wallet_allocation_holds WHERE owner_type='security_incident' AND owner_id=$2 AND status='open')
	`, outflowID.String(), otherOwner).Scan(&externalNetworkOpen, &externalWalletOpen, &independentNetworkOpen, &independentWalletOpen))
			require.Zero(t, externalNetworkOpen)
			require.Zero(t, externalWalletOpen)
			require.Equal(t, 1, independentNetworkOpen)
			require.Equal(t, 1, independentWalletOpen)
			var openIncidents, resolvedIncidents int
			require.NoError(t, pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE status IN ('open','acknowledged')),
		  COUNT(*) FILTER (WHERE status='resolved')
		FROM reg_crypto_reconciliation_items
		WHERE network_id=$1 AND wallet_id=$2 AND kind='external_outflow'
		  AND tx_hash=$3 AND event_kind=$4 AND event_index=$5
	`, networkID, walletID, event.TxHash, event.EventKind, event.EventIndex).Scan(&openIncidents, &resolvedIncidents))
			require.Zero(t, openIncidents)
			require.Equal(t, 1, resolvedIncidents)

		})
	}
}

func newIntegrationExternalOutflowRecorder(txm *postgres.TxManager) crypto.ExternalOutflowRecorder {
	holds := cryptoapp.NewOperationalHoldService(txm, NewOperationalHoldRepo())
	return cryptoapp.NewExternalOutflowDetectionService(
		txm,
		NewExpectedTransferRepo(),
		NewObservedExternalOutflowRepo(),
		NewExternalOutflowReconciliationRunRepo(),
		NewExternalOutflowIncidentRepo(),
		NewExternalOutflowObservationRepo(),
		NewExternalOutflowActionRepo(),
		holds,
	)
}

func runConcurrentOutflowCommands(actors []id.ID, command func(id.ID) error) []error {
	var wg sync.WaitGroup
	errorsByActor := make([]error, len(actors))
	for index, actorID := range actors {
		wg.Add(1)
		go func(index int, actorID id.ID) {
			defer wg.Done()
			errorsByActor[index] = command(actorID)
		}(index, actorID)
	}
	wg.Wait()
	return errorsByActor
}

func requireOneSuccessOneConflict(t *testing.T, results []error) {
	t.Helper()
	var successes, conflicts int
	for _, result := range results {
		if result == nil {
			successes++
			continue
		}
		appErr, ok := apperror.AsAppError(result)
		if ok && appErr.Code == apperror.CodeConflict {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent command error: %v", result)
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}

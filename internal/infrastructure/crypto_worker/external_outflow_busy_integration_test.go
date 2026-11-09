//go:build integration

package crypto_worker

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

func TestExternalOutflowBusyDefersObservationWithoutExhaustingAttempts(t *testing.T) {
	runSweepCoordinationFixture(t, true, func(ctx context.Context, tm *postgres.TxManager, f sweepCoordinationFixture) {
		pool, err := pgxpool.New(ctx, os.Getenv("METAPUS_TEST_CRYPTO_DSN"))
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		var source, contract string
		require.NoError(t, pool.QueryRow(ctx, `SELECT w.address,t.contract_address FROM cat_wallets w,cat_tokens t WHERE w.id=$1 AND t.id=$2`, f.wallet, f.token).Scan(&source, &contract))
		event := crypto.BlockchainEvent{NetworkID: f.network, TxHash: strings.Repeat("a", 64), EventKind: crypto.EventKindContractLog,
			FromAddress: source, ToAddress: "external-destination", TokenContract: contract, Amount: types.MustCryptoAmount("42"),
			BlockNumber: 10, BlockHash: strings.Repeat("b", 64), Confirmations: 1, RequiredConfs: 19, Direction: crypto.TransferDirectionOutgoing}
		repo := crypto_repo.NewChainObservationRepo()
		require.NoError(t, repo.Capture(ctx, event))
		var observation crypto_repo.ChainObservation
		require.NoError(t, pool.QueryRow(ctx, `SELECT id,payload FROM reg_crypto_chain_observations WHERE network_id=$1`, f.network).Scan(&observation.ID, &observation.Payload))
		observation.NetworkID = f.network
		holds := cryptoapp.NewOperationalHoldService(tm, crypto_repo.NewOperationalHoldRepo())
		recorder := cryptoapp.NewExternalOutflowDetectionService(tm, crypto_repo.NewExpectedChainEffectRepo(), crypto_repo.NewObservedExternalOutflowRepo(),
			crypto_repo.NewExternalOutflowReconciliationRunRepo(), crypto_repo.NewExternalOutflowIncidentRepo(), crypto_repo.NewExternalOutflowObservationRepo(), crypto_repo.NewExternalOutflowActionRepo(), holds)
		processor := &CryptoProcessor{workerID: "outflow-busy-test", eventProcessor: crypto.NewEventProcessor(crypto.EventProcessorConfig{TxManager: tm, OutflowRecorder: recorder})}
		control, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = control.Rollback(ctx) }()
		_, err = control.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.wallet)
		require.NoError(t, err)
		claim := func() {
			t.Helper()
			_, err := pool.Exec(ctx, `UPDATE reg_crypto_chain_observations SET status='processing',attempts=8,lease_owner=$2,lease_expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, observation.ID, processor.workerID)
			require.NoError(t, err)
		}
		for range 10 {
			claim()
			limited, cancel := context.WithTimeout(ctx, time.Second)
			err = processor.processClaimedChainObservation(limited, repo, observation)
			cancel()
			require.NoError(t, err)
			var status, reason string
			var attempts int
			var released bool
			require.NoError(t, pool.QueryRow(ctx, `SELECT status,attempts,last_error,lease_owner IS NULL AND lease_expires_at IS NULL FROM reg_crypto_chain_observations WHERE id=$1`, observation.ID).Scan(&status, &attempts, &reason, &released))
			require.Equal(t, "pending", status)
			require.Equal(t, 7, attempts)
			require.Equal(t, "external_outflow_busy", reason)
			require.True(t, released)
		}
		var outflows, reviewHolds int
		require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_external_outflows WHERE network_id=$1),
 (SELECT count(*) FROM reg_crypto_network_holds WHERE network_id=$1 AND reason='chain_observation_needs_review')`, f.network).Scan(&outflows, &reviewHolds))
		require.Zero(t, outflows)
		require.Zero(t, reviewHolds)
		require.NoError(t, control.Rollback(ctx))
		claim()
		require.NoError(t, processor.processClaimedChainObservation(ctx, repo, observation))
		var processed bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT status='processed' FROM reg_crypto_chain_observations WHERE id=$1`, observation.ID).Scan(&processed))
		require.True(t, processed)
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_external_outflows WHERE network_id=$1`, f.network).Scan(&outflows))
		require.Equal(t, 1, outflows)
	})
}

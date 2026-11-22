//go:build integration

package crypto_repo

import (
	"context"
	"strings"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"metapus/pkg/sdk/types"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type outflowFixture110 struct {
	base                   amlRoutingFixture
	network, wallet, asset id.ID
	event                  crypto.BlockchainEvent
}

func newOutflowFixture110(t *testing.T, family string) outflowFixture110 {
	t.Helper()
	ctx, pool := amlTestDatabase(t)
	f := outflowFixture110{base: amlRoutingFixture{ctx: ctx, pool: pool}, network: id.New(), wallet: id.New(), asset: id.New()}
	standard := "TRC-20"
	if family == "evm" {
		standard = "ERC-20"
	}
	f.event = crypto.BlockchainEvent{NetworkID: f.network, TxHash: strings.Repeat("c", 64), EventKind: crypto.EventKindContractLog,
		FromAddress: "source-" + f.wallet.String(), ToAddress: "external-destination", TokenContract: "asset-" + f.asset.String(),
		Amount: types.MustRawAmount("42"), BlockNumber: 100, BlockHash: strings.Repeat("a", 64), Confirmations: 1, RequiredConfs: 19,
		Direction: crypto.TransferDirectionOutgoing}
	_, err := pool.Exec(ctx, `INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
 VALUES($1,$1::uuid::text,'Outflow history',$2,$1::uuid::text,'N',6,FALSE)`, f.network, family)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
 VALUES($1,$1::uuid::text,'Outflow asset',$2,$3,'T',6,$4)`, f.asset, f.network, f.event.TokenContract, standard)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO cat_wallets(id,code,name,network_id,address,tier,wallet_role,status,vault_key_id)
 VALUES($1,$1::uuid::text,'Outflow source',$2,$3,'pool','merchant_pool','free','synthetic-key')`, f.wallet, f.network, f.event.FromAddress)
	require.NoError(t, err)
	return f
}

func (f outflowFixture110) record(t *testing.T, event crypto.BlockchainEvent) {
	t.Helper()
	tm := postgres.MustGetTxManager(f.base.ctx)
	require.NoError(t, newIntegrationExternalOutflowRecorder(tm).RecordExternalOutflow(f.base.ctx, event))
}

func (f outflowFixture110) saved(t *testing.T) (id.ID, string, outflowProjection) {
	t.Helper()
	var key id.ID
	var basis string
	var p outflowProjection
	require.NoError(t, f.base.pool.QueryRow(f.base.ctx, `SELECT id,initial_basis::text,block_number,block_hash,confirmations,required_confs,finality_status
 FROM reg_crypto_external_outflows WHERE network_id=$1`, f.network).Scan(&key, &basis, &p.Height, &p.Hash, &p.Confirmations, &p.Required, &p.Finality))
	return key, basis, p
}

func TestExternalOutflowBasisRetainsFirstObservationAndCoherentBlock(t *testing.T) {
	f := newOutflowFixture110(t, "tron")
	f.record(t, f.event)
	key, basis, first := f.saved(t)
	stale := f.event
	stale.BlockNumber, stale.BlockHash, stale.Confirmations, stale.Finality = 99, strings.Repeat("b", 64), 500, crypto.FinalityFinalized
	f.record(t, stale)
	_, after, projection := f.saved(t)
	require.Equal(t, basis, after)
	require.Equal(t, first, projection, "an older block cannot donate its hash or confirmations to the current block")
	confirmed := f.event
	confirmed.BlockHash, confirmed.Confirmations, confirmed.RequiredConfs, confirmed.Finality = strings.ToUpper(f.event.BlockHash), 20, 21, crypto.FinalitySolidified
	f.record(t, confirmed)
	_, _, projection = f.saved(t)
	require.Equal(t, f.event.BlockHash, projection.Hash)
	require.Equal(t, 21, projection.Required)
	require.Equal(t, 20, projection.Confirmations)
	included := f.event
	included.BlockNumber, included.BlockHash, included.Confirmations, included.RequiredConfs = 101, strings.Repeat("d", 64), 2, 0
	f.record(t, included)
	_, after, projection = f.saved(t)
	require.Equal(t, basis, after)
	require.Equal(t, outflowProjection{Height: 101, Hash: included.BlockHash, Confirmations: 2, Required: 21, Finality: "observed"}, projection)
	bad := included
	bad.BlockNumber++
	require.ErrorContains(t, newIntegrationExternalOutflowRecorder(postgres.MustGetTxManager(f.base.ctx)).RecordExternalOutflow(f.base.ctx, bad), "different height")
	var observations, linked int
	require.NoError(t, f.base.pool.QueryRow(f.base.ctx, `SELECT count(*),count(incident_id) FROM reg_crypto_external_outflow_observations WHERE external_outflow_id=$1`, key).Scan(&observations, &linked))
	require.Equal(t, 4, observations)
	require.Equal(t, observations, linked)
	for _, mutation := range []string{
		`UPDATE reg_crypto_external_outflows SET initial_basis='{}' WHERE id=$1`,
		`UPDATE reg_crypto_external_outflows SET from_address='another source' WHERE id=$1`,
		`UPDATE reg_crypto_external_outflows SET detected_at=clock_timestamp() WHERE id=$1`,
		`UPDATE reg_crypto_external_outflows SET required_confs=0 WHERE id=$1`,
	} {
		_, err := f.base.pool.Exec(f.base.ctx, mutation, key)
		require.Error(t, err)
	}
	_, after, _ = f.saved(t)
	require.Equal(t, basis, after)
	_, err := f.base.pool.Exec(f.base.ctx, `INSERT INTO reg_crypto_external_outflow_observations(external_outflow_id,run_id,network_id,tx_hash,event_kind,event_index,block_number,block_hash,confirmations,required_confs,finality_status,observation_source)
 SELECT external_outflow_id,run_id,network_id,tx_hash,event_kind,event_index,block_number,block_hash,confirmations,required_confs,finality_status,observation_source
 FROM reg_crypto_external_outflow_observations WHERE external_outflow_id=$1 LIMIT 1`, key)
	require.ErrorContains(t, err, "source incident and run")
}

func TestExternalOutflowReplayRetainsRetiredCatalogOwners(t *testing.T) {
	f := newOutflowFixture110(t, "evm")
	f.record(t, f.event)
	key, basis, _ := f.saved(t)
	_, err := f.base.pool.Exec(f.base.ctx, `UPDATE cat_tokens SET deletion_mark=TRUE,is_active=FALSE WHERE id=$1`, f.asset)
	require.NoError(t, err)
	_, err = f.base.pool.Exec(f.base.ctx, `UPDATE cat_wallets SET deletion_mark=TRUE,is_active=FALSE WHERE id=$1`, f.wallet)
	require.NoError(t, err)
	_, err = f.base.pool.Exec(f.base.ctx, `INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
 VALUES($1,$1::uuid::text,'Later alias',$2,$3,'T',6,'ERC-20')`, id.New(), f.network, f.event.TokenContract)
	require.NoError(t, err)
	f.record(t, f.event)
	_, after, _ := f.saved(t)
	require.Equal(t, basis, after)
	var wallet, asset id.ID
	require.NoError(t, f.base.pool.QueryRow(f.base.ctx, `SELECT source_wallet_id,token_id FROM reg_crypto_external_outflows WHERE id=$1`, key).Scan(&wallet, &asset))
	require.Equal(t, f.wallet, wallet)
	require.Equal(t, f.asset, asset)
	// A raw INSERT refresh also retains the first record and clock.
	var before, replay string
	require.NoError(t, f.base.pool.QueryRow(f.base.ctx, `SELECT to_jsonb(o)::text FROM reg_crypto_external_outflows o WHERE id=$1`, key).Scan(&before))
	_, err = f.base.pool.Exec(f.base.ctx, `INSERT INTO reg_crypto_external_outflows(network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,source_address_key,destination_address_key,contract_address,contract_address_key,amount,block_number,block_hash)
 SELECT network_id,token_id,source_wallet_id,tx_hash,event_kind,event_index,from_address,to_address,source_address_key,destination_address_key,contract_address,contract_address_key,amount,999,'later-hint'
 FROM reg_crypto_external_outflows WHERE id=$1 ON CONFLICT DO NOTHING`, key)
	require.NoError(t, err)
	require.NoError(t, f.base.pool.QueryRow(f.base.ctx, `SELECT to_jsonb(o)::text FROM reg_crypto_external_outflows o WHERE id=$1`, key).Scan(&replay))
	require.Equal(t, before, replay)
}

func TestExternalOutflowResourcePrefixFailsWithoutPartialWrites(t *testing.T) {
	f := newOutflowFixture110(t, "tron")
	ctx, pool := f.base.ctx, f.base.pool
	control, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = control.Rollback(ctx) }()
	_, err = control.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.wallet)
	require.NoError(t, err)
	limited, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err = newIntegrationExternalOutflowRecorder(postgres.MustGetTxManager(ctx)).RecordExternalOutflow(limited, f.event)
	require.ErrorContains(t, err, "resources are busy")
	require.ErrorIs(t, err, crypto.ErrExternalOutflowBusy)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_external_outflows WHERE network_id=$1`, f.network).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, control.Rollback(ctx))
	f.record(t, f.event)
	key, _, _ := f.saved(t)
	control, err = pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = control.Rollback(ctx) }()
	_, err = control.Exec(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, f.wallet)
	require.NoError(t, err)
	tm := postgres.MustGetTxManager(ctx)
	err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
		_, err := NewExternalOutflowWorkflowRepo().GetExternalOutflow(ctx, key, true)
		return err
	})
	require.ErrorContains(t, err, "resources are busy")
	require.ErrorIs(t, err, crypto.ErrExternalOutflowBusy)
	_, err = control.Exec(ctx, `UPDATE reg_crypto_external_outflows SET confirmations=confirmations+1 WHERE id=$1`, key)
	require.NoError(t, err, "failed workflow releases its identity gate before the catalog owner writes")
	require.NoError(t, control.Rollback(ctx))
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE reg_crypto_external_outflows SET confirmations=confirmations+1 WHERE id=$1`, key)
	require.ErrorContains(t, err, "read committed")
}

func TestExternalOutflowObservationsRejectForeignSources(t *testing.T) {
	f := newOutflowFixture110(t, "tron")
	f.record(t, f.event)
	key, _, _ := f.saved(t)
	ctx, pool := f.base.ctx, f.base.pool
	var run, incident id.ID
	require.NoError(t, pool.QueryRow(ctx, `SELECT run_id,incident_id FROM reg_crypto_external_outflow_observations WHERE external_outflow_id=$1`, key).Scan(&run, &incident))
	detected := cryptoapp.DetectedExternalOutflow{OutflowID: key, NetworkID: f.network, WalletID: f.wallet, TokenID: &f.asset, Status: crypto.ExternalOutflowPendingReview}
	tm := postgres.MustGetTxManager(ctx)
	for _, change := range []func(*crypto.BlockchainEvent){
		func(e *crypto.BlockchainEvent) { e.FromAddress = "other-source" },
		func(e *crypto.BlockchainEvent) { e.ToAddress = "other-destination" },
		func(e *crypto.BlockchainEvent) { e.TokenContract = "other-contract" },
		func(e *crypto.BlockchainEvent) { e.Amount = types.MustRawAmount("43") },
	} {
		bad := f.event
		change(&bad)
		err := tm.RunInTransaction(ctx, func(ctx context.Context) error {
			return NewExternalOutflowObservationRepo().AppendExternalOutflowObservations(ctx, run, incident, detected, bad)
		})
		require.ErrorContains(t, err, "observed source")
		err = tm.RunInTransaction(ctx, func(ctx context.Context) error {
			_, err := NewExternalOutflowIncidentRepo().UpsertExternalOutflowIncident(ctx, run, detected, bad)
			return err
		})
		require.ErrorContains(t, err, "observed source")
	}
	var observations, occurrences int
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM reg_crypto_external_outflow_observations WHERE external_outflow_id=$1),
 (SELECT occurrence_count FROM reg_crypto_reconciliation_items WHERE id=$2)`, key, incident).Scan(&observations, &occurrences))
	require.Equal(t, 1, observations)
	require.Equal(t, 1, occurrences)
}

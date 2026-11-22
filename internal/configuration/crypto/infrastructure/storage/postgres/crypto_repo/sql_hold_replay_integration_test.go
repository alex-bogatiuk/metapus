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
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/postgres"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func insertRawHold(ctx context.Context, c cryptoapp.AcquireOperationalHoldCommand) (int64, error) {
	table, column, err := operationalHoldTable(c.Scope)
	if err != nil {
		return 0, err
	}
	details, err := json.Marshal(c.Details)
	if err != nil {
		return 0, err
	}
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO `+table+`(`+column+`,reason,owner_type,owner_id,details,held_by) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, c.ScopeID, c.Reason, c.OwnerType, c.OwnerID, string(details), c.HeldBy)
	return tag.RowsAffected(), err
}

func TestSQLHoldOwnerKeyAndBusyMatchPreviousGoProtocol(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	for _, owner := range [][2]string{{"incident", "plain"}, {"a|b", "c"}, {"a", "b|c"}, {"quotes\"\\", "строка\n<>\u2028"}} {
		key, err := json.MarshalDeterministic([]string{"operational-hold", owner[0], owner[1]})
		require.NoError(t, err)
		var sqlKey string
		require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT fn_crypto_hold_owner_key($1,$2)`, owner[0], owner[1]).Scan(&sqlKey))
		require.Equal(t, string(key), sqlKey)
	}
	command := holdCommand(f, cryptoapp.OperationalHoldScopeNetwork, id.New().String())
	key, err := json.MarshalDeterministic([]string{"operational-hold", command.OwnerType, command.OwnerID})
	require.NoError(t, err)
	tm := postgres.MustGetTxManager(f.Ctx)
	require.NoError(t, tm.RunInTransaction(f.Ctx, func(ctx context.Context) error {
		_, err := tm.GetQuerier(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(key))
		if err != nil {
			return err
		}
		bounded, cancel := context.WithTimeout(f.Ctx, time.Second)
		defer cancel()
		_, err = insertRawHold(bounded, command)
		var pg *pgconn.PgError
		require.ErrorAs(t, err, &pg)
		require.Equal(t, "55P03", pg.Code)
		require.ErrorContains(t, NewOperationalHoldRepo().AcquireOperationalHold(bounded, command), "owner is busy")
		return nil
	}))
	count, err := insertRawHold(f.Ctx, command)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestSQLHoldInsertAndResolutionReplayPreserveHistory(t *testing.T) {
	f := NewActivationSelectorFixture(t)
	owner := id.New().String()
	network, wallet := holdCommand(f, cryptoapp.OperationalHoldScopeNetwork, owner), holdCommand(f, cryptoapp.OperationalHoldScopeWallet, owner)
	for _, c := range []cryptoapp.AcquireOperationalHoldCommand{network, wallet} {
		count, err := insertRawHold(f.Ctx, c)
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
	}
	before := holdSnapshot(t, f)
	for _, column := range []string{"reason", "held_by", "details", "status", "resolved_by", "resolution"} {
		columns := []string{"reason", "held_by", "details", "status", "resolved_by", "resolution"}
		values := []string{"'external_outflow'", "'detector'", "'{\"source\":\"verified\",\"amount\":\"100\"}'::jsonb", "'open'", "''", "''"}
		for i, name := range columns {
			if name == column {
				values[i] = "NULL"
			}
		}
		_, err := f.Pool.Exec(f.Ctx, `INSERT INTO reg_crypto_network_holds(network_id,owner_type,owner_id,`+strings.Join(columns, ",")+`) VALUES($1,$2,$3,`+strings.Join(values, ",")+`)`, f.Network, network.OwnerType, owner)
		require.Error(t, err, "NULL field must not bypass replay validation: %s", column)
		require.Equal(t, before, holdSnapshot(t, f))
	}
	for _, c := range []cryptoapp.AcquireOperationalHoldCommand{network, wallet} {
		count, err := insertRawHold(f.Ctx, c)
		require.NoError(t, err)
		require.Zero(t, count)
		c.HeldBy = "changed-actor"
		_, err = insertRawHold(f.Ctx, c)
		require.ErrorContains(t, err, "original acquisition")
		require.Equal(t, before, holdSnapshot(t, f))
	}
	_, err := f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_wallet_allocation_holds SET resolved_by='invented' WHERE owner_id=$1`, owner)
	require.ErrorContains(t, err, "open operational hold")
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_network_holds SET id=$2 WHERE owner_id=$1`, owner, id.New())
	require.ErrorContains(t, err, "immutable")
	_, err = f.Pool.Exec(f.Ctx, `DELETE FROM reg_crypto_wallet_allocation_holds WHERE owner_id=$1`, owner)
	require.ErrorContains(t, err, "immutable")
	require.Equal(t, before, holdSnapshot(t, f))
	var start time.Time
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT clock_timestamp()`).Scan(&start))
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_network_holds SET status='resolved',resolved_by='reviewer',resolution='verified',resolved_at='2000-01-01' WHERE owner_id=$1`, owner)
	require.NoError(t, err)
	var resolvedAt time.Time
	require.NoError(t, f.Pool.QueryRow(f.Ctx, `SELECT resolved_at FROM reg_crypto_network_holds WHERE owner_id=$1`, owner).Scan(&resolvedAt))
	require.False(t, resolvedAt.Before(start))
	mixed := holdSnapshot(t, f)
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_by='other',resolution='verified',resolved_at=clock_timestamp() WHERE owner_id=$1`, owner)
	require.ErrorContains(t, err, "original actor or reason")
	require.Equal(t, mixed, holdSnapshot(t, f))
	_, err = f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_wallet_allocation_holds SET status='resolved',resolved_by='reviewer',resolution='verified',resolved_at=clock_timestamp() WHERE owner_id=$1`, owner)
	require.NoError(t, err)
	terminal := holdSnapshot(t, f)
	tag, err := f.Pool.Exec(f.Ctx, `UPDATE reg_crypto_network_holds SET status='resolved',resolved_by='reviewer',resolution='verified',resolved_at=clock_timestamp() WHERE owner_id=$1`, owner)
	require.NoError(t, err)
	require.Zero(t, tag.RowsAffected())
	count, err := insertRawHold(f.Ctx, wallet)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, terminal, holdSnapshot(t, f))
	wallet.ScopeID = f.B
	_, err = insertRawHold(f.Ctx, wallet)
	require.ErrorContains(t, err, "cannot acquire another target")
	require.Equal(t, terminal, holdSnapshot(t, f))
	require.NoError(t, postgres.MustGetTxManager(f.Ctx).RunInTransactionWithOptions(f.Ctx, postgres.SerializableTxOptions(), func(ctx context.Context) error {
		// A savepoint is necessary because this is a direct SQL constraint failure.
		err := postgres.MustGetTxManager(ctx).RunInSavepoint(ctx, func(ctx context.Context) error {
			_, err := insertRawHold(ctx, network)
			require.ErrorContains(t, err, "read committed")
			return err
		})
		require.ErrorContains(t, err, "read committed")
		return nil
	}))
}

func provisionSQLHoldWallets(t *testing.T, count int) (amlRoutingFixture, crypto.MerchantPoolPolicy, []id.ID) {
	t.Helper()
	f, p := automaticPoolFixture(t, "merchant_pool", count, count+3)
	_, _, err := f.repo.ReconcileNextMerchantPool(f.ctx)
	require.NoError(t, err)
	service := cryptoapp.NewMerchantWalletProvisioningService(f.repo, automaticPoolVault(t))
	wallets := make([]id.ID, 0, count)
	for range count {
		claimed, wallet, err := service.RunNext(f.ctx, "sql-hold-control")
		require.NoError(t, err)
		require.True(t, claimed)
		wallets = append(wallets, wallet)
	}
	return f, p, wallets
}

func TestAMLPoolScopeHoldsSeparateWalletsAndFreezeFirstVersion(t *testing.T) {
	f, p, wallets := provisionSQLHoldWallets(t, 2)
	update := func() {
		t.Helper()
		_, err := f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_routing_scopes SET version=version+1 WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3`, p.MerchantID, p.NetworkID, f.input.Request.TokenID)
		require.NoError(t, err)
	}
	update()
	var count, owners int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*),count(DISTINCT owner_id) FROM reg_crypto_wallet_allocation_holds WHERE owner_type='aml_pool_scope_baseline'`).Scan(&count, &owners))
	require.Equal(t, 2, count)
	require.Equal(t, 2, owners)
	snapshot := func() string {
		t.Helper()
		var value string
		require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT jsonb_agg(to_jsonb(h) ORDER BY wallet_id,id)::text FROM reg_crypto_wallet_allocation_holds h WHERE owner_type='aml_pool_scope_baseline'`).Scan(&value))
		return value
	}
	first := snapshot()
	update()
	require.Equal(t, first, snapshot())
	var owner string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT owner_id FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND owner_type='aml_pool_scope_baseline'`, wallets[0]).Scan(&owner))
	_, err := NewOperationalHoldRepo().ResolveOperationalHolds(f.ctx, "aml_pool_scope_baseline", owner, "reviewer", "verified")
	require.NoError(t, err)
	update()
	var otherOpen, firstClosed bool
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND owner_type='aml_pool_scope_baseline' AND status='resolved'),
 EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$2 AND owner_type='aml_pool_scope_baseline' AND status='open')`, wallets[0], wallets[1]).Scan(&firstClosed, &otherOpen))
	require.True(t, firstClosed)
	require.True(t, otherOpen)
}

func TestAMLPoolLegacyScopeHoldKeepsOriginalIdentity(t *testing.T) {
	f, p, wallets := provisionSQLHoldWallets(t, 1)
	wallet := wallets[0]
	key := id.New()
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_wallet_allocation_holds(id,wallet_id,reason,owner_type,owner_id,details,held_by)
 VALUES($1,$2,'aml_scope_baseline_missing','aml_pool_scope_baseline',$3::uuid::text,jsonb_build_object('tokenId',$3::uuid::text,'scopeVersion',1),'aml-pool-controller')`, key, wallet, f.input.Request.TokenID)
	require.NoError(t, err)
	var before, after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE id=$1`, key).Scan(&before))
	_, err = f.pool.Exec(f.ctx, `UPDATE sys_crypto_aml_routing_scopes SET version=version+1 WHERE merchant_id=$1 AND network_id=$2 AND token_id=$3`, p.MerchantID, p.NetworkID, f.input.Request.TokenID)
	require.NoError(t, err)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE id=$1`, key).Scan(&after))
	require.Equal(t, before, after)
}

func TestAMLPoolHighRiskNewFindingGetsNewHold(t *testing.T) {
	f, p, wallets := provisionSQLHoldWallets(t, 1)
	wallet := wallets[0]
	_, err := f.pool.Exec(f.ctx, `INSERT INTO reg_crypto_wallet_risk_states(wallet_id,merchant_id,network_id,token_id,risk_class,revision) VALUES($1,$2,$3,$4,'high',1)`, wallet, p.MerchantID, p.NetworkID, f.input.Request.TokenID)
	require.NoError(t, err)
	var owner, first string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT owner_id,to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1 AND owner_type='aml_pool_risk'`, wallet).Scan(&owner, &first))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET revision=1 WHERE wallet_id=$1 AND token_id=$2`, wallet, f.input.Request.TokenID)
	require.NoError(t, err)
	_, err = NewOperationalHoldRepo().ResolveOperationalHolds(f.ctx, "aml_pool_risk", owner, "reviewer", "verified")
	require.NoError(t, err)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET revision=1 WHERE wallet_id=$1 AND token_id=$2`, wallet, f.input.Request.TokenID)
	require.NoError(t, err)
	var count int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND owner_type='aml_pool_risk'`, wallet).Scan(&count))
	require.Equal(t, 1, count)
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET revision=2 WHERE wallet_id=$1 AND token_id=$2`, wallet, f.input.Request.TokenID)
	require.NoError(t, err)
	var second string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1 AND owner_type='aml_pool_risk' AND status='open'`, wallet).Scan(&second))
	_, err = f.pool.Exec(f.ctx, `UPDATE reg_crypto_wallet_risk_states SET revision=3 WHERE wallet_id=$1 AND token_id=$2`, wallet, f.input.Request.TokenID)
	require.NoError(t, err)
	var after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1 AND owner_type='aml_pool_risk' AND status='open'`, wallet).Scan(&after))
	require.Equal(t, second, after)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT count(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND owner_type='aml_pool_risk'`, wallet).Scan(&count))
	require.Equal(t, 2, count)
	require.NotEmpty(t, first)
}

func TestAMLPreflightHoldsKeepFirstObservationAndNewReview(t *testing.T) {
	f, _, wallet := prepareAutomaticBaseline(t)
	save := func(height int64, hashChar, amount string) {
		t.Helper()
		job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "sql-preflight", time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		proof := checkpointForJob(job, amount)
		proof.Height = height
		proof.Hash = strings.Repeat(hashChar, 64)
		for i := range proof.Sources {
			proof.Sources[i].Height = height
			proof.Sources[i].Hash = proof.Hash
		}
		require.NoError(t, f.repo.SaveAMLPreflight(f.ctx, job, proof))
	}
	retry := func() {
		t.Helper()
		_, err := f.pool.Exec(f.ctx, `UPDATE reg_crypto_aml_preflight_jobs SET status='retry_wait',next_attempt_at=clock_timestamp()-interval '1 second' WHERE wallet_id=$1`, wallet)
		require.NoError(t, err)
	}
	save(30, "a", "5")
	var owner, first, after string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT owner_id,to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1 AND owner_type='aml_preflight' AND status='open'`, wallet).Scan(&owner, &first))
	retry()
	save(31, "b", "6")
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT to_jsonb(h)::text FROM reg_crypto_wallet_allocation_holds h WHERE wallet_id=$1 AND owner_type='aml_preflight' AND status='open'`, wallet).Scan(&after))
	require.Equal(t, first, after)
	_, err := NewOperationalHoldRepo().ResolveOperationalHolds(f.ctx, "aml_preflight", owner, "reviewer", "verified")
	require.NoError(t, err)
	retry()
	save(32, "c", "7")
	var holds, observations int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM reg_crypto_wallet_allocation_holds WHERE wallet_id=$1 AND owner_type='aml_preflight'),(SELECT count(*) FROM reg_crypto_aml_preflight_observations WHERE wallet_id=$1)`, wallet).Scan(&holds, &observations))
	require.Equal(t, 2, holds)
	require.Equal(t, 3, observations)
}

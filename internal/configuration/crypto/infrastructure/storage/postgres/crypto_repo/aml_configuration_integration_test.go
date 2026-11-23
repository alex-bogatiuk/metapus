//go:build integration

package crypto_repo

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAMLConfigurationSnapshotReportsMissingNativeAndNamespaceWithoutMutations(t *testing.T) {
	f, _, namespace := merchantProvisionFixture(t)
	var before, after int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_outbox`).Scan(&before))
	report, err := f.repo.ReadAMLConfiguration(f.ctx, "", namespace)
	require.NoError(t, err)
	require.Equal(t, cryptoschema.ExpectedVersion, report.DatabaseSchema)
	require.True(t, report.NamespaceEnrolled)
	require.Empty(t, report.Blockers)
	require.Len(t, report.Networks, 1)
	n := report.Networks[0]
	require.Equal(t, f.input.Request.NetworkID, n.Network.ID)
	require.Equal(t, 1, n.MissingNativeScopes)
	require.Contains(t, n.Blockers, "native_scopes_missing")
	require.False(t, n.DispatchEnabled)
	wrong, err := f.repo.ReadAMLConfiguration(f.ctx, n.Network.Code, id.New())
	require.NoError(t, err)
	require.Contains(t, wrong.Blockers, "vault_namespace_not_enrolled")
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_outbox`).Scan(&after))
	require.Equal(t, before, after)
	// A stronger read-only snapshot cannot silently reuse a caller's weaker tx.
	require.Error(t, postgres.MustGetTxManager(f.ctx).RunInTransaction(f.ctx, func(ctx context.Context) error { _, err := f.repo.ReadAMLConfiguration(ctx, "", namespace); return err }))
}

func TestAMLConfigurationScopesAreFilteredAndOldSchemaNeverReadsNewTables(t *testing.T) {
	f, _, namespace := merchantProvisionFixture(t)
	report, err := f.repo.ReadAMLConfiguration(f.ctx, "does-not-exist", namespace)
	require.NoError(t, err)
	require.Contains(t, report.Blockers, "no_aml_networks_found")
	// Only synthetic migration metadata is changed; no protected AML row is
	// altered. The report must stop before querying schema-specific relations.
	_, err = f.pool.Exec(f.ctx, `UPDATE goose_db_version_crypto SET is_applied=FALSE WHERE version_id=$1`, cryptoschema.ExpectedVersion)
	require.NoError(t, err)
	report, err = f.repo.ReadAMLConfiguration(f.ctx, "", namespace)
	require.NoError(t, err)
	require.Equal(t, []string{"schema_incompatible"}, report.Blockers)
	require.Empty(t, report.Networks)
}

func TestAMLConfigurationFindsAutomaticWalletBeforeBaselineJobExists(t *testing.T) {
	f, _, wallet := prepareAutomaticBaseline(t)
	var namespace id.ID
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT vault_namespace FROM sys_crypto_aml_provisioning_identity WHERE id`).Scan(&namespace))
	report, err := f.repo.ReadAMLConfiguration(f.ctx, "", namespace)
	require.NoError(t, err)
	require.Len(t, report.Networks, 1)
	require.Equal(t, 1, report.Networks[0].UnbaselinedAutomaticWallets)
	require.Zero(t, report.Networks[0].BaselinePending)
	require.Contains(t, report.Networks[0].Blockers, "automatic_wallet_baselines_missing")
	job, ok, err := f.repo.ClaimAMLPreflight(f.ctx, "fixture", time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, wallet, job.WalletID)
	require.NoError(t, f.repo.SaveAMLPreflight(f.ctx, job, checkpointForJob(job, "0")))
	report, err = f.repo.ReadAMLConfiguration(f.ctx, "", namespace)
	require.NoError(t, err)
	require.Zero(t, report.Networks[0].UnbaselinedAutomaticWallets)
	require.Zero(t, report.Networks[0].BaselinePending)
	// Once the empty opening is established, receiving funds can make current
	// inventory incomplete; it does not erase the immutable opening baseline.
	_, err = f.pool.Exec(f.ctx, "INSERT INTO reg_crypto_balance_movements(recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount) VALUES($1,'ConfigurationFixture',1,NOW(),'receipt',$2,$3,1)", id.New(), wallet, job.TokenID)
	require.NoError(t, err)
	var emptyReady bool
	require.NoError(t, f.pool.QueryRow(f.ctx, "SELECT fn_crypto_aml_pool_baselines_ready(id) FROM reg_crypto_aml_wallet_provisions WHERE wallet_id=$1", wallet).Scan(&emptyReady))
	require.False(t, emptyReady)
	report, err = f.repo.ReadAMLConfiguration(f.ctx, "", namespace)
	require.NoError(t, err)
	require.Zero(t, report.Networks[0].UnbaselinedAutomaticWallets, "funded inventory must not be reported as a missing opening")
}

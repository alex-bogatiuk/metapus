//go:build integration

package crypto_integration_repo

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/blockchain_network"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/rpcconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/catalog_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/security"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func rpcManagementFixture(t *testing.T) (context.Context, *pgxpool.Pool, id.ID, []*rpcconfig.RPCEndpoint) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	ctx, db := fixtureDB(t)
	var network id.ID
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE chain_family='evm' LIMIT 1`).Scan(&network))
	require.NoError(t, func() error {
		_, err := db.Exec(ctx, `UPDATE cat_blockchain_networks SET rpc_config_source='file' WHERE id=$1`, network)
		return err
	}())
	repo := NewCryptoRPCEndpointRepo()
	var endpoints []*rpcconfig.RPCEndpoint
	for index, provider := range []string{"first", "second", "reserve"} {
		e := rpcconfig.NewRPCEndpoint("RPC-"+provider, provider)
		e.NetworkID, e.ProviderID, e.TrustGroup, e.URL, e.Priority = network, provider, provider, "https://"+provider+".example.test/", (index+1)*10
		e.SetAttribute("operatorNote", "preserve-on-reorder")
		require.NoError(t, repo.Create(ctx, e))
		endpoints = append(endpoints, e)
	}
	_, err := db.Exec(ctx, `UPDATE cat_blockchain_networks SET rpc_config_source='database',version=version+1 WHERE id=$1`, network)
	require.NoError(t, err)
	return ctx, db, network, endpoints
}

func managementUpdate(snapshot rpcconfig.RPCConfiguration, changes ...rpcconfig.RPCConfigurationChange) rpcconfig.RPCConfigurationUpdate {
	return rpcconfig.RPCConfigurationUpdate{ExpectedRevision: snapshot.Revision, Endpoints: changes}
}

func managementApply(repo rpcconfig.RPCConfigurationRepository, ctx context.Context, network id.ID, update rpcconfig.RPCConfigurationUpdate) (rpcconfig.RPCConfiguration, error) {
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return rpcconfig.RPCConfiguration{}, err
	}
	return cryptoapp.NewRPCConfigurationService(repo, txm, nil).Update(ctx, network, update)
}

func TestRPCManagementAtomicDisableRestoreAndSharedRevision(t *testing.T) {
	ctx, _, network, endpoints := rpcManagementFixture(t)
	repo := NewRPCConfigurationRepo("production")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, 3, before.Requirements[0].ConfiguredProviders)
	encoded, err := json.Marshal(before)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "example.test")
	loaded, err := LoadRPCNetwork(ctx, crypto.NetworkRef{ID: network}, chainconfig.Network{})
	require.NoError(t, err)
	require.Equal(t, before.EffectiveRevision, loaded.Revision("evm", 2))
	after, err := managementApply(repo, ctx, network, managementUpdate(before,
		rpcconfig.RPCConfigurationChange{ID: endpoints[0].ID, Version: 1, IsActive: true, Priority: 20},
		rpcconfig.RPCConfigurationChange{ID: endpoints[1].ID, Version: 1, IsActive: true, Priority: 10},
		rpcconfig.RPCConfigurationChange{ID: endpoints[2].ID, Version: 1, IsActive: false, Priority: 30},
	))
	require.NoError(t, err)
	require.Equal(t, endpoints[1].ID, after.Endpoints[0].ID)
	require.Equal(t, 2, after.Requirements[0].ConfiguredProviders)
	require.False(t, after.Endpoints[2].IsActive)
	require.Len(t, after.Endpoints, 3)
	loaded, err = LoadRPCNetwork(ctx, crypto.NetworkRef{ID: network}, chainconfig.Network{})
	require.NoError(t, err)
	require.Equal(t, after.EffectiveRevision, loaded.Revision("evm", 2))
	require.Len(t, loaded.Roles[chainconfig.RoleRPC], 2)
	_, err = managementApply(repo, ctx, network, managementUpdate(before))
	require.Error(t, err)
	reserve := after.Endpoints[2]
	restored, err := managementApply(repo, ctx, network, managementUpdate(after, rpcconfig.RPCConfigurationChange{ID: reserve.ID, Version: reserve.Version, IsActive: true, Priority: reserve.Priority}))
	require.NoError(t, err)
	require.True(t, restored.Endpoints[2].IsActive)
	stored, err := NewCryptoRPCEndpointRepo().GetByID(ctx, reserve.ID)
	require.NoError(t, err)
	require.Equal(t, endpoints[2].URL, stored.URL)
	require.Equal(t, endpoints[2].Slot, stored.Slot)
	require.Equal(t, "preserve-on-reorder", stored.Attributes["operatorNote"])
	noop, err := managementApply(repo, ctx, network, managementUpdate(restored))
	require.NoError(t, err)
	require.Equal(t, restored.Revision, noop.Revision)
}

type rpcAuditFailure struct{}

func (rpcAuditFailure) WriteBatch(context.Context, []eventlog.Event) error {
	return errors.New("audit unavailable")
}

func (rpcAuditFailure) Write(context.Context, eventlog.Event) error {
	return errors.New("audit unavailable")
}

func TestRPCManagementRollsBackInvalidCoverageAndAuditFailure(t *testing.T) {
	ctx, _, network, endpoints := rpcManagementFixture(t)
	repo := NewRPCConfigurationRepo("production")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	_, err = managementApply(repo, ctx, network, managementUpdate(before,
		rpcconfig.RPCConfigurationChange{ID: endpoints[0].ID, Version: 1, IsActive: false, Priority: 10},
		rpcconfig.RPCConfigurationChange{ID: endpoints[1].ID, Version: 1, IsActive: false, Priority: 20},
	))
	require.Error(t, err)
	after, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
	txm, err := tenant.GetTxManager(ctx)
	require.NoError(t, err)
	svc := cryptoapp.NewRPCConfigurationService(repo, txm, rpcAuditFailure{})
	_, err = svc.Update(ctx, network, managementUpdate(before, rpcconfig.RPCConfigurationChange{ID: endpoints[2].ID, Version: 1, IsActive: false, Priority: 30}))
	require.ErrorContains(t, err, "audit unavailable")
	after, err = repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
	_, err = managementApply(repo, ctx, network, managementUpdate(before, rpcconfig.RPCConfigurationChange{ID: id.New(), Version: 1, IsActive: false, Priority: 10}))
	require.Error(t, err)
}

func TestRPCManagementConcurrentCommandsAndCatalogGuards(t *testing.T) {
	ctx, _, network, endpoints := rpcManagementFixture(t)
	repo := NewRPCConfigurationRepo("production")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, index := range []int{0, 2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := endpoints[index]
			_, err := managementApply(repo, ctx, network, managementUpdate(before, rpcconfig.RPCConfigurationChange{ID: e.ID, Version: 1, IsActive: false, Priority: e.Priority}))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	after, err := repo.Get(ctx, network)
	require.NoError(t, err)
	active := make([]id.ID, 0, 2)
	for _, e := range after.Endpoints {
		if e.IsActive {
			active = append(active, e.ID)
		}
	}
	require.Len(t, active, 2)
	catalog := NewCryptoRPCEndpointRepo()
	require.Error(t, catalog.Delete(ctx, active[0]))
	require.Error(t, catalog.SetDeletionMark(ctx, active[0], true))
	e, err := catalog.GetByID(ctx, active[0])
	require.NoError(t, err)
	e.IsActive = false
	require.Error(t, catalog.Update(ctx, e))
	unchanged, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, after.Revision, unchanged.Revision)
	// Insertion changes the snapshot even though every existing row kept its version.
	added := rpcconfig.NewRPCEndpoint("RPC-new", "new")
	added.NetworkID, added.ProviderID, added.TrustGroup, added.URL = network, "new", "new", "https://new.example.test/"
	require.NoError(t, catalog.Create(ctx, added))
	_, err = managementApply(repo, ctx, network, managementUpdate(after))
	require.Error(t, err)
}

func TestRPCManagementFileModeAndDevelopmentCoverage(t *testing.T) {
	ctx, db, network, endpoints := rpcManagementFixture(t)
	t.Setenv("APP_ENV", "development")
	repo := NewRPCConfigurationRepo("development")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, 1, before.Requirements[0].RequiredProviders)
	_, err = managementApply(repo, ctx, network, managementUpdate(before,
		rpcconfig.RPCConfigurationChange{ID: endpoints[0].ID, Version: 1, IsActive: false, Priority: 10},
		rpcconfig.RPCConfigurationChange{ID: endpoints[1].ID, Version: 1, IsActive: false, Priority: 20},
	))
	require.NoError(t, err)
	_, err = db.Exec(ctx, `UPDATE cat_blockchain_networks SET rpc_config_source='file',version=version+1 WHERE id=$1`, network)
	require.NoError(t, err)
	file, err := repo.Get(ctx, network)
	require.NoError(t, err)
	_, err = managementApply(repo, ctx, network, managementUpdate(file))
	require.ErrorContains(t, err, "файл")
}

func TestRPCManagementTronValidatesEachRoleIndependently(t *testing.T) {
	ctx, db, _, _ := rpcManagementFixture(t)
	tron := blockchain_network.NewBlockchainNetwork("RPC-MANAGEMENT-TRON", "TRON", crypto.ChainFamilyTRON, "nile", "TRX", 6)
	networks := catalog_repo.NewCatalogRepoForDefinition[*blockchain_network.BlockchainNetwork](blockchain_network.Definition,
		postgres.ExtractDBColumns[blockchain_network.BlockchainNetwork](),
		func() *blockchain_network.BlockchainNetwork { return &blockchain_network.BlockchainNetwork{} })
	require.NoError(t, networks.Create(ctx, tron))
	network := tron.ID
	_, err := db.Exec(ctx, `UPDATE cat_blockchain_networks SET rpc_config_source='file' WHERE id=$1`, network)
	require.NoError(t, err)
	catalog := NewCryptoRPCEndpointRepo()
	var victim *rpcconfig.RPCEndpoint
	for _, role := range []string{"fullnode", "solidity"} {
		for _, provider := range []string{"node-one", "node-two"} {
			e := rpcconfig.NewRPCEndpoint(role+"-"+provider, role+" "+provider)
			e.NetworkID, e.Role, e.ProviderID, e.TrustGroup, e.URL = network, rpcconfig.Role(role), provider, provider, "https://"+provider+".example.test/"
			require.NoError(t, catalog.Create(ctx, e))
			victim = e
		}
	}
	_, err = db.Exec(ctx, `UPDATE cat_blockchain_networks SET rpc_config_source='database',version=version+1 WHERE id=$1`, network)
	require.NoError(t, err)
	repo := NewRPCConfigurationRepo("production")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Len(t, before.Requirements, 2)
	for _, role := range before.Requirements {
		require.Equal(t, 2, role.ConfiguredProviders)
	}
	_, err = managementApply(repo, ctx, network, managementUpdate(before, rpcconfig.RPCConfigurationChange{ID: victim.ID, Version: 1, IsActive: false, Priority: 0}))
	require.Error(t, err)
	after, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
}

func TestRPCManagementHonorsFieldPolicies(t *testing.T) {
	ctx, _, network, endpoints := rpcManagementFixture(t)
	repo := NewRPCConfigurationRepo("production")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	readDenied := security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"crypto_rpc_endpoint:read": {AllowedFields: []string{"*", "-name"}}})
	_, err = repo.Get(readDenied, network)
	require.Error(t, err)
	writeDenied := security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"crypto_rpc_endpoint:write": {AllowedFields: []string{"*", "-is_active"}}})
	_, err = managementApply(repo, writeDenied, network, managementUpdate(before, rpcconfig.RPCConfigurationChange{ID: endpoints[2].ID, Version: 1, IsActive: false, Priority: 30}))
	require.Error(t, err)
	after, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
}

func TestRPCSettingsAndEndpointPacingShareAtomicRevision(t *testing.T) {
	ctx, db, network, endpoints := rpcManagementFixture(t)
	repo := NewRPCConfigurationRepo("production")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	settings := before.Settings
	settings.EVM = &rpcconfig.EVMSettings{AttemptTimeoutMS: 10000, ReadBudgetMS: 25000, ProviderConcurrency: 12, BackfillConcurrency: 6}
	settings.Alerts = rpcconfig.IngestionAlertSettings{MaxLagSeconds: 120, RecoveryLagSeconds: 20, FireAfterSeconds: 90, RecoverAfterSeconds: 180}
	interval := 500
	update := managementUpdate(before, rpcconfig.RPCConfigurationChange{ID: endpoints[0].ID, Version: 1, IsActive: true, Priority: 10, MinRequestIntervalMS: &interval})
	update.Settings = &settings
	after, err := managementApply(repo, ctx, network, update)
	require.NoError(t, err)
	require.NotEqual(t, before.Revision, after.Revision)
	require.NotEqual(t, before.EffectiveRevision, after.EffectiveRevision)
	require.Equal(t, 12, after.EffectiveSettings.EVM.ProviderConcurrency)
	require.Equal(t, 500, after.Endpoints[0].MinRequestIntervalMS)
	loaded, err := LoadRPCNetwork(ctx, crypto.NetworkRef{ID: network}, chainconfig.Network{})
	require.NoError(t, err)
	require.Equal(t, after.EffectiveRevision, loaded.Revision("evm", 2))
	alerts, err := LoadIngestionAlertSettings(ctx)
	require.NoError(t, err)
	var code string
	require.NoError(t, db.QueryRow(ctx, `SELECT code FROM cat_blockchain_networks WHERE id=$1`, network).Scan(&code))
	require.Equal(t, settings.Alerts, alerts[code])
	_, err = managementApply(repo, ctx, network, update)
	require.Error(t, err, "stale full snapshot cannot overwrite settings")
	// A valid endpoint edit rolls back with an invalid settings object.
	invalid := after.Settings
	invalid.Alerts.RecoveryLagSeconds = 200
	bad := managementUpdate(after, rpcconfig.RPCConfigurationChange{ID: endpoints[0].ID, Version: after.Endpoints[0].Version, IsActive: false, Priority: 10})
	bad.Settings = &invalid
	_, err = managementApply(repo, ctx, network, bad)
	require.Error(t, err)
	unchanged, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, after.Revision, unchanged.Revision)
	// Settings-only writes are audited, and an audit failure rolls back SQL.
	settings.Alerts.FireAfterSeconds = 120
	update = managementUpdate(after)
	update.Settings = &settings
	tm, err := tenant.GetTxManager(ctx)
	require.NoError(t, err)
	_, err = cryptoapp.NewRPCConfigurationService(repo, tm, rpcAuditFailure{}).Update(ctx, network, update)
	require.Error(t, err)
	unchanged, err = repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, after.Revision, unchanged.Revision)
	after, err = managementApply(repo, ctx, network, update)
	require.NoError(t, err)
	require.Equal(t, unchanged.EffectiveRevision, after.EffectiveRevision, "alert policy alone does not reset RPC circuits")
}

func TestRPCSettingsCannotBypassEndpointFieldRestrictions(t *testing.T) {
	ctx, _, network, _ := rpcManagementFixture(t)
	repo := NewRPCConfigurationRepo("production")
	before, err := repo.Get(ctx, network)
	require.NoError(t, err)
	settings := before.Settings
	settings.Alerts.FireAfterSeconds = 90
	update := managementUpdate(before)
	update.Settings = &settings
	denied := security.WithFieldPolicies(ctx, map[string]*security.FieldPolicy{"crypto_rpc_endpoint:write": {AllowedFields: []string{"*", "-min_request_interval_ms"}}})
	_, err = managementApply(repo, denied, network, update)
	require.Error(t, err)
	after, err := repo.Get(ctx, network)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
}

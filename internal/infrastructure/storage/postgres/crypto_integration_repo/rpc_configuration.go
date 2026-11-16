package crypto_integration_repo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/security"
	"metapus/internal/domain/crypto/rpcconfig"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
)

type RPCConfigurationRepo struct {
	environment string
	policy      *security.PolicyEngine
}

func NewRPCConfigurationRepo(environment string, engines ...*security.PolicyEngine) *RPCConfigurationRepo {
	repo := &RPCConfigurationRepo{environment: strings.ToLower(strings.TrimSpace(environment))}
	if len(engines) > 0 {
		repo.policy = engines[0]
	}
	return repo
}

type rpcConfigurationState struct {
	view    rpcconfig.RPCConfiguration
	records []*rpcconfig.RPCEndpoint
}

// A single SQL statement gives the network, settings and complete endpoint set
// one MVCC snapshot, including inactive records and their versions.
func (r *RPCConfigurationRepo) load(ctx context.Context, network id.ID) (rpcConfigurationState, error) {
	var state rpcConfigurationState
	var source, family, identifier string
	var version, decoder int
	var evm, tron, alerts, raw []byte
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	err := q.QueryRow(ctx, `SELECT n.rpc_config_source,n.chain_family,n.network_identifier,n.version,
	 COALESCE(s.evm_settings,'null'::jsonb),COALESCE(s.tron_settings,'null'::jsonb),COALESCE(s.ingestion_alert_settings,'{}'::jsonb),
	 COALESCE((SELECT decoder_version FROM sys_crypto_block_ingestion WHERE network_id=n.id),2),
	 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',e.id,'code',e.code,'name',e.name,'version',e.version,
	 'attributes',e.attributes,'parentId',e.parent_id,'isFolder',e.is_folder,'deletionMark',e.deletion_mark,'createdAt',e.created_at,'updatedAt',e.updated_at,
	 'networkId',e.network_id,'role',e.role,'providerId',e.provider_id,'trustGroup',e.trust_group,'url',e.url,
	 'poolId',e.pool_id,'credentialSlot',e.credential_slot,'priority',e.priority,'minRequestIntervalMs',e.min_request_interval_ms,
	 'disableKeepAlives',e.disable_keep_alives,'isActive',e.is_active) ORDER BY e.priority,e.id)
	 FROM sys_crypto_rpc_endpoints e WHERE e.network_id=n.id AND NOT e.deletion_mark AND e._deleted_at IS NULL),'[]'::jsonb)
	 FROM cat_blockchain_networks n LEFT JOIN sys_crypto_rpc_network_settings s ON s.network_id=n.id
	 WHERE n.id=$1 AND NOT n.deletion_mark AND n._deleted_at IS NULL`, network).Scan(&source, &family, &identifier, &version, &evm, &tron, &alerts, &decoder, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, apperror.NewNotFound("blockchain_network", network.String())
	}
	if err != nil {
		return state, fmt.Errorf("load RPC configuration: %w", err)
	}
	if err := json.Unmarshal(raw, &state.records); err != nil {
		return state, fmt.Errorf("decode RPC configuration: %w", err)
	}
	state.view = rpcconfig.RPCConfiguration{NetworkID: network, Family: family, Source: source, Endpoints: []rpcconfig.RPCConfigurationEndpoint{}, Requirements: []rpcconfig.RPCRoleRequirement{}}
	topology := rpcconfig.RPCTopology{Roles: make(map[string][]rpcconfig.RPCConfiguredEndpoint)}
	if err := json.Unmarshal(evm, &topology.EVM); err != nil {
		return state, err
	}
	if err := json.Unmarshal(tron, &topology.TRON); err != nil {
		return state, err
	}
	state.view.Settings.EVM, state.view.Settings.TRON = topology.EVM, topology.TRON
	if err := json.Unmarshal(alerts, &state.view.Settings.Alerts); err != nil {
		return state, fmt.Errorf("decode ingestion alert settings: %w", err)
	}
	state.view.EffectiveSettings = state.view.Settings.Defaults(family)
	state.view.DefaultSettings = (rpcconfig.RPCNetworkSettings{}).Defaults(family)
	for _, e := range state.records {
		endpoint := rpcconfig.RPCConfiguredEndpoint{EndpointID: e.ID, ConfigVersion: e.Version, ProviderID: e.ProviderID, TrustGroup: e.TrustGroup, URL: e.URL, Priority: e.Priority, MinRequestIntervalMS: e.MinRequestIntervalMS, DisableKeepAlives: e.DisableKeepAlives}
		state.view.Endpoints = append(state.view.Endpoints, rpcconfig.RPCConfigurationEndpoint{ID: e.ID, Version: e.Version, Name: e.Name, Role: e.Role, ProviderID: e.ProviderID, TrustGroup: rpcconfig.RPCTrustIdentity(e.URL, e.TrustGroup, e.ProviderID), IsActive: e.IsActive, Priority: e.Priority, MinRequestIntervalMS: e.MinRequestIntervalMS})
		if e.IsActive {
			topology.Roles[e.Role] = append(topology.Roles[e.Role], endpoint)
		}
	}
	state.view.Requirements = rpcconfig.RPCRequirements(family, r.environment, state.view.Endpoints)
	encoded, err := json.MarshalDeterministic(struct {
		NetworkVersion                          int
		Source, Family, Identifier, Environment string
		Decoder                                 int
		EVM, TRON, Alerts                       json.RawMessage
		Endpoints                               []*rpcconfig.RPCEndpoint
	}{version, source, family, identifier, r.environment, decoder, evm, tron, alerts, state.records})
	if err != nil {
		return state, err
	}
	state.view.Revision = fmt.Sprintf("%x", sha256.Sum256(encoded))
	if source == "database" {
		state.view.EffectiveRevision = topology.Revision(family, decoder)
	}
	return state, nil
}

// ValidateActiveBindings re-validates the active endpoints of a network whose
// RPC topology is read from the database; other sources need no check.
func (r *RPCConfigurationRepo) ValidateActiveBindings(ctx context.Context, network id.ID) error {
	state, err := r.load(ctx, network)
	if err != nil {
		return err
	}
	if state.view.Source != "database" {
		return nil
	}
	for _, endpoint := range state.records {
		if endpoint.IsActive {
			if err := NewCryptoRPCEndpointRepo().ValidateBinding(ctx, endpoint); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RPCConfigurationRepo) Get(ctx context.Context, network id.ID) (rpcconfig.RPCConfiguration, error) {
	state, err := r.load(ctx, network)
	if err == nil {
		err = r.authorizeRead(ctx, state.records)
	}
	return state.view, err
}

// A partial topology cannot be presented as a complete editable snapshot.
func (r *RPCConfigurationRepo) authorizeRead(ctx context.Context, records []*rpcconfig.RPCEndpoint) error {
	if policy := security.GetFieldPolicy(ctx, "crypto_rpc_endpoint", "read"); policy != nil {
		for _, field := range []string{"id", "version", "network_id", "name", "role", "provider_id", "trust_group", "is_active", "priority", "min_request_interval_ms"} {
			if !policy.IsFieldAllowed(field) {
				return apperror.NewForbidden("Недостаточно прав для просмотра полной конфигурации RPC")
			}
		}
	}
	for _, record := range records {
		if err := security.CheckRLSAccess(ctx, "crypto_rpc_endpoint", record); err != nil {
			return err
		}
		if err := security.CheckCELPolicy(ctx, r.policy, "crypto_rpc_endpoint", "read", record); err != nil {
			return err
		}
	}
	return nil
}

// Lock serializes topology writes across the management API and catalog paths.
func (r *RPCConfigurationRepo) Lock(ctx context.Context) error {
	return catalog_repo.LockIntegrationConfiguration(ctx)
}

// Save persists a plan produced under the caller's transaction and configuration
// lock. SQL/security checks remain here; order and coverage policy live in domain.
func (r *RPCConfigurationRepo) Save(ctx context.Context, network id.ID, expectedRevision string, changes []rpcconfig.RPCConfigurationChange, settings *rpcconfig.RPCNetworkSettings) (rpcconfig.RPCConfiguration, error) {
	state, err := r.load(ctx, network)
	if err != nil {
		return rpcconfig.RPCConfiguration{}, err
	}
	if state.view.Revision != expectedRevision {
		return rpcconfig.RPCConfiguration{}, apperror.NewConcurrentModification("crypto_rpc_configuration", network.String())
	}
	byID := make(map[id.ID]*rpcconfig.RPCEndpoint, len(state.records))
	for _, e := range state.records {
		byID[e.ID] = e
	}
	repo := NewCryptoRPCEndpointRepo()
	for _, change := range changes {
		e := byID[change.ID]
		if e == nil || e.Version != change.Version {
			return rpcconfig.RPCConfiguration{}, apperror.NewConcurrentModification("crypto_rpc_configuration", network.String())
		}
		current := *e
		e.IsActive, e.Priority = change.IsActive, change.Priority
		if change.MinRequestIntervalMS != nil {
			e.MinRequestIntervalMS = *change.MinRequestIntervalMS
		}
		if err := security.CheckRLSAccess(ctx, "crypto_rpc_endpoint", e); err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
		if err := security.CheckCELPolicy(ctx, r.policy, "crypto_rpc_endpoint", "update", &current); err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
		if err := security.ValidateWrite(&current, e, security.GetFieldPolicy(ctx, "crypto_rpc_endpoint", "write")); err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
		if err := repo.BaseCatalogRepo.Update(ctx, e); err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
	}
	if settings != nil {
		if policy := security.GetFieldPolicy(ctx, "crypto_rpc_endpoint", "write"); policy != nil {
			for _, field := range []string{"priority", "is_active", "min_request_interval_ms"} {
				if !policy.IsFieldAllowed(field) {
					return rpcconfig.RPCConfiguration{}, apperror.NewForbidden("Недостаточно прав для изменения настроек RPC")
				}
			}
		}
		if err := settings.Validate(state.view.Family); err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
		// Reuse the same scope/CEL permissions as the endpoint command.
		for _, e := range state.records {
			if err := security.CheckRLSAccess(ctx, "crypto_rpc_endpoint", e); err != nil {
				return rpcconfig.RPCConfiguration{}, err
			}
			if err := security.CheckCELPolicy(ctx, r.policy, "crypto_rpc_endpoint", "update", e); err != nil {
				return rpcconfig.RPCConfiguration{}, err
			}
		}
		evm, err := json.Marshal(settings.EVM)
		if err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
		tron, err := json.Marshal(settings.TRON)
		if err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
		alerts, err := json.Marshal(settings.Alerts)
		if err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
		_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO sys_crypto_rpc_network_settings(network_id,evm_settings,tron_settings,ingestion_alert_settings) VALUES($1,$2,$3,$4) ON CONFLICT(network_id) DO UPDATE SET evm_settings=EXCLUDED.evm_settings,tron_settings=EXCLUDED.tron_settings,ingestion_alert_settings=EXCLUDED.ingestion_alert_settings,version=sys_crypto_rpc_network_settings.version+1,updated_at=NOW()`, network, evm, tron, alerts)
		if err != nil {
			return rpcconfig.RPCConfiguration{}, fmt.Errorf("save RPC network settings: %w", err)
		}
	}
	// Cross-binding constraints must observe the complete final transaction image.
	for _, e := range state.records {
		if err := repo.ValidateBinding(ctx, e); err != nil {
			return rpcconfig.RPCConfiguration{}, err
		}
	}
	return r.Get(ctx, network)
}

func ValidateRPCNetworkMutation(ctx context.Context, network id.ID, mutate func(context.Context) error) error {
	repo := NewRPCConfigurationRepo(os.Getenv("APP_ENV"))
	beforeState, err := repo.load(ctx, network)
	if err != nil {
		return err
	}
	if err := mutate(ctx); err != nil {
		return err
	}
	afterState, err := repo.load(ctx, network)
	if err != nil {
		return err
	}
	return rpcconfig.ValidateRPCCoverageChange(beforeState.view, afterState.view)
}

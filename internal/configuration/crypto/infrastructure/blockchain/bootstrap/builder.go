// Package bootstrap is the chain-family adapter composition root. It is the
// only non-TRON package allowed to import the TRON adapter.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/evm"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/tron"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_integration_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

type Builder struct {
	config *chainconfig.File
	appEnv string
}

// NewFamilyCapabilityRegistry returns metadata for every family adapter linked
// into this binary. It deliberately requires no RPC or secret configuration.
func NewFamilyCapabilityRegistry() (*crypto.ChainFamilyCapabilityRegistry, error) {
	return crypto.BuildChainFamilyCapabilityRegistry([]crypto.ChainFamilyCapabilityProvider{
		&tron.Factory{},
		&evm.Factory{},
	})
}

func NewBuilder(configFile string, appEnv ...string) (*Builder, error) {
	cfg := &chainconfig.File{Networks: make(map[string]chainconfig.Network)}
	if strings.TrimSpace(configFile) != "" {
		var err error
		cfg, err = chainconfig.Load(configFile)
		if err != nil {
			return nil, err
		}
	}
	environment := "development"
	if len(appEnv) > 0 && strings.TrimSpace(appEnv[0]) != "" {
		environment = strings.ToLower(strings.TrimSpace(appEnv[0]))
	}
	return &Builder{config: cfg, appEnv: environment}, nil
}

func (b *Builder) Build(ctx context.Context, networks []crypto.NetworkRef, derivers crypto.FamilyWalletDeriverResolver) (*crypto.ChainDriverRegistry, error) {
	if b == nil || b.config == nil {
		return nil, fmt.Errorf("chain driver builder is not configured")
	}
	if derivers == nil {
		return nil, fmt.Errorf("family wallet deriver registry is required")
	}
	var buildErrors []error
	configuredNetworks := make([]crypto.NetworkRef, 0, len(networks))
	tronConfigs := make(map[id.ID]tron.DriverConfig)
	evmConfigs := make(map[id.ID]evm.DriverConfig)
	for _, network := range networks {
		fallback := b.config.Networks[network.Code]
		runtime, loadErr := crypto_integration_repo.LoadRPCNetwork(ctx, network, fallback)
		if loadErr != nil {
			buildErrors = append(buildErrors, loadErr)
			continue
		}
		if len(runtime.Roles) == 0 {
			buildErrors = append(buildErrors, fmt.Errorf("runtime endpoints missing for network code %s", network.Code))
			continue
		}
		var failureThreshold, openSeconds int
		var maxFee types.RawAmount
		if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
			SELECT rpc_failure_threshold,rpc_open_seconds,per_transaction_max_fee_minor
			FROM sys_crypto_network_policies WHERE network_id=$1 AND _deleted_at IS NULL
		`, network.ID).Scan(&failureThreshold, &openSeconds, &maxFee); err != nil {
			buildErrors = append(buildErrors, fmt.Errorf("load RPC policy for %s: %w", network.Code, err))
			continue
		}
		deriver, err := derivers.ResolveDeriver(network.Family)
		if err != nil {
			buildErrors = append(buildErrors, fmt.Errorf("resolve wallet deriver for %s: %w", network.Code, err))
			continue
		}
		switch network.Family {
		case crypto.ChainFamilyTRON:
			roles := []chainconfig.Role{chainconfig.RoleFullNode, chainconfig.RoleSolidity}
			if err := runtime.RequireRoles(roles...); err != nil {
				buildErrors = append(buildErrors, fmt.Errorf("runtime endpoints for %s: %w", network.Code, err))
				continue
			}
			settings := chainconfig.TRONSettings{}
			if runtime.TRON != nil {
				settings = *runtime.TRON
			}
			var activated bool
			if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE network_id=$1 AND activated_at IS NOT NULL)`, network.ID).Scan(&activated); err != nil {
				buildErrors = append(buildErrors, fmt.Errorf("read block ingestion policy for %s: %w", network.Code, err))
				continue
			}
			if activated && settings.Defaults().IngestionMode == "shadow" {
				buildErrors = append(buildErrors, fmt.Errorf("activated network %s cannot downgrade ingestion policy", network.Code))
				continue
			}
			loadDecoderVersion := func(ctx context.Context) (int, error) {
				var version int
				err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT COALESCE((SELECT decoder_version FROM sys_crypto_block_ingestion WHERE network_id=$1),2)`, network.ID).Scan(&version)
				if err == nil && version != crypto.TRONTransferEvidenceSchema {
					err = crypto.ErrDecoderVersionConflict
				}
				return version, err
			}
			decoderVersion, err := loadDecoderVersion(ctx)
			if err != nil {
				buildErrors = append(buildErrors, fmt.Errorf("read TRON decoder version: %w", err))
				continue
			}
			poolConfig := tron.RPCPoolConfig{
				Settings:    settings,
				NetworkCode: network.Code, FailureThreshold: failureThreshold, OpenDuration: time.Duration(openSeconds) * time.Second,
				FullNode: runtime.Endpoints(chainconfig.RoleFullNode), Solidity: runtime.Endpoints(chainconfig.RoleSolidity),
				InitialVersion: fmt.Sprintf("%s/decoder:%d", rpcVersion(runtime), decoderVersion),
			}
			poolConfig.Reload = func(ctx context.Context) (tron.RPCPoolConfig, string, error) {
				next, err := crypto_integration_repo.LoadRPCNetwork(ctx, network, fallback)
				if err != nil {
					return tron.RPCPoolConfig{}, "", err
				}
				nextDecoderVersion, err := loadDecoderVersion(ctx)
				if err != nil {
					return tron.RPCPoolConfig{}, "", err
				}
				version := fmt.Sprintf("%s/decoder:%d", rpcVersion(next), nextDecoderVersion)
				if nextDecoderVersion != decoderVersion {
					return tron.RPCPoolConfig{}, version, crypto.ErrDecoderVersionConflict
				}
				if err = next.RequireRoles(roles...); err != nil {
					return tron.RPCPoolConfig{}, version, err
				}
				c := poolConfig
				c.Reload = nil
				c.FullNode = next.Endpoints(chainconfig.RoleFullNode)
				c.Solidity = next.Endpoints(chainconfig.RoleSolidity)
				// Watcher mode changes require the existing controlled restart procedure.
				if next.TRON != nil && next.TRON.Defaults().IngestionMode != settings.Defaults().IngestionMode {
					return c, version, fmt.Errorf("ingestion mode change requires watcher restart")
				}
				if next.TRON != nil {
					c.Settings = *next.TRON
				}
				return c, version, nil
			}
			pool, err := tron.NewRPCPool(poolConfig)
			if err != nil {
				buildErrors = append(buildErrors, fmt.Errorf("build RPC pool for %s: %w", network.Code, err))
				continue
			}
			if maxFee.Cmp(tron.DefaultProtocolMaxFee()) < 0 {
				maxFee = tron.DefaultProtocolMaxFee()
			}
			tronConfigs[network.ID] = tron.DriverConfig{
				Network: network, RPC: pool, WatcherStateRepository: crypto_repo.NewWatcherStateRepo(), WalletDeriver: deriver,
				BlockCaptureRepository: crypto_repo.NewBlockIngestionRepo(),
				MaxFeeMinor:            maxFee, QuoteTTL: 30 * time.Second,
			}
		case crypto.ChainFamilyEVM:
			if err := runtime.RequireRoles(chainconfig.RoleRPC); err != nil {
				buildErrors = append(buildErrors, fmt.Errorf("runtime endpoints for %s: %w", network.Code, err))
				continue
			}
			if b.appEnv == "production" && runtime.IndependentProviders(chainconfig.RoleRPC) < 2 {
				buildErrors = append(buildErrors, fmt.Errorf("production EVM network %s requires at least two independent RPC providers", network.Code))
				continue
			}
			chainID, ok := new(big.Int).SetString(network.NetworkIdentifier, 10)
			if !ok || chainID.Sign() <= 0 {
				buildErrors = append(buildErrors, fmt.Errorf("invalid EVM chain ID for %s", network.Code))
				continue
			}
			settings := chainconfig.EVMSettings{}
			if runtime.EVM != nil {
				settings = *runtime.EVM
			}
			poolConfig := evm.RPCPoolConfig{NetworkCode: network.Code, ExpectedChainID: chainID, Settings: settings, Endpoints: runtime.Endpoints(chainconfig.RoleRPC), FailureThreshold: failureThreshold, OpenDuration: time.Duration(openSeconds) * time.Second, InitialVersion: rpcVersion(runtime)}
			poolConfig.Reload = func(ctx context.Context) (evm.RPCPoolConfig, string, error) {
				next, err := crypto_integration_repo.LoadRPCNetwork(ctx, network, fallback)
				if err != nil {
					return evm.RPCPoolConfig{}, "", err
				}
				version := rpcVersion(next)
				if err = next.RequireRoles(chainconfig.RoleRPC); err != nil {
					return evm.RPCPoolConfig{}, version, err
				}
				if b.appEnv == "production" && next.IndependentProviders(chainconfig.RoleRPC) < 2 {
					return evm.RPCPoolConfig{}, version, fmt.Errorf("two independent RPC providers required")
				}
				c := poolConfig
				c.Reload = nil
				c.Endpoints = next.Endpoints(chainconfig.RoleRPC)
				if next.EVM != nil {
					c.Settings = *next.EVM
				}
				return c, version, nil
			}
			pool, err := evm.NewRPCPool(ctx, poolConfig)
			if err != nil {
				buildErrors = append(buildErrors, fmt.Errorf("build EVM RPC pool for %s: %w", network.Code, err))
				continue
			}
			if maxFee.Cmp(evm.DefaultProtocolMaxFee()) < 0 {
				maxFee = evm.DefaultProtocolMaxFee()
			}
			watcherRepo := crypto_repo.NewWatcherStateRepo()
			evmConfigs[network.ID] = evm.DriverConfig{Network: network, RPC: pool,
				WatcherStateRepository: watcherRepo, WatcherBlockHistory: watcherRepo, NetworkHoldRecorder: crypto_repo.NewNetworkHoldRepo(), WalletDeriver: deriver,
				NonceManager: evm.NewPostgresNonceManager(), MaxFeeMinor: maxFee,
				QuoteTTL: 30 * time.Second, WatcherPollInterval: 3 * time.Second, WatcherLookback: 64, WatcherSettings: settings}
		default:
			buildErrors = append(buildErrors, fmt.Errorf("unsupported family %s for network %s", network.Family, network.Code))
			continue
		}
		configuredNetworks = append(configuredNetworks, network)
	}
	factories := []crypto.DriverFactory{&tron.Factory{Configs: tronConfigs}, &evm.Factory{Configs: evmConfigs}}
	registry, registryErr := crypto.BuildChainDriverRegistry(ctx, configuredNetworks, factories)
	buildErrors = append(buildErrors, registryErr)
	for networkID, cfg := range tronConfigs {
		if _, err := registry.Resolve(networkID); err != nil {
			cfg.RPC.Close()
		}
	}
	for networkID, cfg := range evmConfigs {
		if _, err := registry.Resolve(networkID); err != nil {
			cfg.RPC.Close()
		}
	}
	return registry, errors.Join(buildErrors...)
}

var _ crypto.ChainDriverRegistryBuilder = (*Builder)(nil)

func rpcVersion(n chainconfig.Network) string {
	return n.Revision("", 0)
}

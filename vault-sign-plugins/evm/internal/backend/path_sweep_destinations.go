package backend

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"vault-plugin-secrets-evm/internal/evm"
	"vault-plugin-secrets-evm/internal/json"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

const sweepDestinationsSchemaV1 = "evm_sweep_destinations.v1"

type sweepDestinationsConfig struct {
	SchemaVersion string              `json:"schema_version"`
	Version       string              `json:"version"`
	Destinations  map[string][]string `json:"destinations"`
}

func pathSweepDestinations(b *evmBackend) []*framework.Path {
	return []*framework.Path{{
		Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/config/sweep-destinations",
		Fields: map[string]*framework.FieldSchema{
			"tenant_id":      {Type: framework.TypeString},
			"schema_version": {Type: framework.TypeString, Required: true},
			"version":        {Type: framework.TypeString, Required: true},
			"destinations":   {Type: framework.TypeMap, Required: true, Description: "Network code to derived consolidation address list"},
		},
		Operations: map[logical.Operation]framework.OperationHandler{
			logical.UpdateOperation: &framework.PathOperation{Callback: b.pathSweepDestinationsWrite},
			logical.ReadOperation:   &framework.PathOperation{Callback: b.pathSweepDestinationsRead},
		},
		HelpSynopsis: "Configures tenant-derived consolidation destinations allowed for sweep",
	}}
}

func (b *evmBackend) pathSweepDestinationsWrite(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	b.signMu.Lock()
	defer b.signMu.Unlock()
	tenantID := strings.TrimSpace(data.Get("tenant_id").(string))
	if !tenantIDPattern.MatchString(tenantID) {
		return logical.ErrorResponse("tenant_id must be a canonical UUID"), nil
	}
	if data.Get("schema_version").(string) != sweepDestinationsSchemaV1 {
		return logical.ErrorResponse("schema_version must be %s", sweepDestinationsSchemaV1), nil
	}
	version := strings.TrimSpace(data.Get("version").(string))
	if version == "" {
		return logical.ErrorResponse("version is required"), nil
	}
	destinations, err := decodeDestinationMap(data.Get("destinations"))
	if err != nil || len(destinations) == 0 {
		return logical.ErrorResponse("destinations must be a non-empty network address map"), nil
	}
	for network, addresses := range destinations {
		if network != evm.SepoliaChainID {
			return logical.ErrorResponse("unsupported sweep destination network: %s", network), nil
		}
		if len(addresses) == 0 {
			return logical.ErrorResponse("network %s has no sweep destinations", network), nil
		}
		seen := map[string]bool{}
		for i, destination := range addresses {
			normalized, normalizeErr := evm.NormalizeAddress(destination)
			if normalizeErr != nil {
				return logical.ErrorResponse("invalid sweep destination address"), nil
			}
			addresses[i] = normalized
			if seen[normalized] {
				return logical.ErrorResponse("duplicate sweep destination address"), nil
			}
			seen[normalized] = true
			if err := requireDerivedTenantWallet(ctx, req.Storage, tenantID, network, normalized); err != nil {
				return logical.ErrorResponse("invalid sweep destination: %v", err), nil
			}
		}
		slices.Sort(addresses)
		destinations[network] = addresses
	}
	cfg := sweepDestinationsConfig{SchemaVersion: sweepDestinationsSchemaV1, Version: version, Destinations: destinations}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if err := req.Storage.Put(ctx, &logical.StorageEntry{Key: sweepDestinationsConfigKey(tenantID), Value: payload, SealWrap: true}); err != nil {
		b.Logger().Error("failed to save sweep destinations", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("failed to save sweep destinations"), nil
	}
	return &logical.Response{Data: map[string]any{"schema_version": cfg.SchemaVersion, "version": cfg.Version, "configured": true}}, nil
}

func (b *evmBackend) pathSweepDestinationsRead(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	cfg, err := b.getSweepDestinationsConfig(ctx, req.Storage, data.Get("tenant_id").(string))
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if cfg.SchemaVersion != sweepDestinationsSchemaV1 {
		return logical.ErrorResponse("sweep destinations are not configured"), nil
	}
	return &logical.Response{Data: map[string]any{"schema_version": cfg.SchemaVersion, "version": cfg.Version, "destinations": cfg.Destinations}}, nil
}

func (b *evmBackend) getSweepDestinationsConfig(ctx context.Context, storage logical.Storage, tenantID string) (sweepDestinationsConfig, error) {
	var cfg sweepDestinationsConfig
	entry, err := storage.Get(ctx, sweepDestinationsConfigKey(tenantID))
	if err != nil || entry == nil {
		return cfg, err
	}
	err = json.Unmarshal(entry.Value, &cfg)
	return cfg, err
}

func (b *evmBackend) validateSweepDestination(ctx context.Context, storage logical.Storage, tenantID, network, destination string) error {
	cfg, err := b.getSweepDestinationsConfig(ctx, storage, tenantID)
	if err != nil {
		return err
	}
	if cfg.SchemaVersion != sweepDestinationsSchemaV1 || !slices.Contains(cfg.Destinations[network], destination) {
		return fmt.Errorf("destination is not allowed by tenant sweep policy")
	}
	return requireDerivedTenantWallet(ctx, storage, tenantID, network, destination)
}

func requireDerivedTenantWallet(ctx context.Context, storage logical.Storage, tenantID, network, address string) error {
	entry, err := storage.Get(ctx, walletStorageKey(tenantID, address))
	if err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("destination is not a derived tenant wallet")
	}
	var wallet map[string]string
	if err := json.Unmarshal(entry.Value, &wallet); err != nil {
		return err
	}
	if wallet["tenant_id"] != tenantID || wallet["chain_id"] != network || wallet["derivation_path"] == "" || wallet["vault_key_id"] == "" {
		return fmt.Errorf("destination wallet scope does not match tenant and network")
	}
	return nil
}

func decodeDestinationMap(raw any) (map[string][]string, error) {
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result map[string][]string
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Called under signMu before publishing a new immutable liquidity_hot binding.
// A failed second write cannot authorize an unowned address; an idempotent
// replay of an existing binding never restores an operator-removed destination.
func (b *evmBackend) registerOwnedCollectorDestination(ctx context.Context, storage logical.Storage, tenantID, network, address string) error {
	custody, err := b.getMerchantCustodyConfig(ctx, storage, tenantID)
	if err != nil {
		return err
	}
	if !slices.Contains(custody.EnforcedNetworks, network) {
		return nil
	}
	cfg, err := b.getSweepDestinationsConfig(ctx, storage, tenantID)
	if err != nil {
		return err
	}
	if cfg.SchemaVersion != "" && cfg.SchemaVersion != sweepDestinationsSchemaV1 {
		return fmt.Errorf("invalid sweep destinations schema")
	}
	if slices.Contains(cfg.Destinations[network], address) {
		return nil
	}
	if cfg.Destinations == nil {
		cfg.Destinations = map[string][]string{}
	}
	cfg.SchemaVersion = sweepDestinationsSchemaV1
	cfg.Version = fmt.Sprintf("custody-%d", time.Now().UnixNano())
	cfg.Destinations[network] = append(cfg.Destinations[network], address)
	slices.Sort(cfg.Destinations[network])
	payload, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return storage.Put(ctx, &logical.StorageEntry{Key: sweepDestinationsConfigKey(tenantID), Value: payload, SealWrap: true})
}

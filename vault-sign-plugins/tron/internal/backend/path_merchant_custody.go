package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"

	"vault-plugin-secrets-tron/internal/json"
	"vault-plugin-secrets-tron/internal/tron"
)

const merchantCustodySchemaV1 = "merchant_custody.v1"

type merchantCustodyConfig struct {
	SchemaVersion    string   `json:"schema_version"`
	Revision         int      `json:"revision"`
	EnforcedNetworks []string `json:"enforced_networks"`
}

type merchantWalletCustody struct {
	SchemaVersion string `json:"schema_version"`
	Network       string `json:"network"`
	Address       string `json:"address"`
	MerchantID    string `json:"merchant_id"`
	Role          string `json:"role"`
}

func pathMerchantCustody(b *tronBackend) []*framework.Path {
	return []*framework.Path{
		{
			Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/config/merchant-custody",
			Fields: map[string]*framework.FieldSchema{
				"tenant_id":         {Type: framework.TypeString},
				"schema_version":    {Type: framework.TypeString, Required: true},
				"revision":          {Type: framework.TypeInt, Required: true},
				"enforced_networks": {Type: framework.TypeSlice, Required: true},
			},
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.UpdateOperation: &framework.PathOperation{Callback: b.pathMerchantCustodyWrite},
				logical.ReadOperation:   &framework.PathOperation{Callback: b.pathMerchantCustodyRead},
			},
			HelpSynopsis: "Enables immutable merchant custody enforcement per network; control-plane only",
		},
		{
			Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/wallets/custody",
			Fields: map[string]*framework.FieldSchema{
				"tenant_id":   {Type: framework.TypeString},
				"network":     {Type: framework.TypeString, Required: true},
				"address":     {Type: framework.TypeString, Required: true},
				"merchant_id": {Type: framework.TypeString},
				"role":        {Type: framework.TypeString, Required: true},
			},
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.UpdateOperation: &framework.PathOperation{Callback: b.pathWalletCustodyWrite},
				logical.ReadOperation:   &framework.PathOperation{Callback: b.pathWalletCustodyRead},
			},
			HelpSynopsis: "Binds an already derived wallet to one immutable merchant/role; control-plane only",
		},
	}
}

func merchantCustodyConfigKey(tenantID string) string {
	return tenantStorageKey(tenantID, "config", "merchant-custody")
}

func merchantWalletCustodyKey(tenantID, network, address string) string {
	digest := sha256.Sum256([]byte(network + "\x00" + address))
	return tenantStorageKey(tenantID, "merchant-wallet-custody", hex.EncodeToString(digest[:]))
}

func normalizeCustodyAddress(network, address string) (string, error) {
	if !validCustodyNetwork(network) || !tron.ValidateAddress(address) {
		return "", fmt.Errorf("unsupported custody network/address")
	}
	return address, nil
}

func (b *tronBackend) getMerchantCustodyConfig(ctx context.Context, storage logical.Storage, tenantID string) (merchantCustodyConfig, error) {
	var cfg merchantCustodyConfig
	entry, err := storage.Get(ctx, merchantCustodyConfigKey(tenantID))
	if err != nil || entry == nil {
		return cfg, err
	}
	if err = json.Unmarshal(entry.Value, &cfg); err != nil {
		return cfg, err
	}
	if cfg.SchemaVersion != merchantCustodySchemaV1 || cfg.Revision <= 0 {
		return cfg, fmt.Errorf("invalid merchant custody configuration")
	}
	return cfg, nil
}

func (b *tronBackend) getWalletCustody(ctx context.Context, storage logical.Storage, tenantID, network, address string) (*merchantWalletCustody, error) {
	normalized, err := normalizeCustodyAddress(network, address)
	if err != nil {
		return nil, err
	}
	entry, err := storage.Get(ctx, merchantWalletCustodyKey(tenantID, network, normalized))
	if err != nil || entry == nil {
		return nil, err
	}
	var binding merchantWalletCustody
	if err = json.Unmarshal(entry.Value, &binding); err != nil {
		return nil, err
	}
	if binding.SchemaVersion != merchantCustodySchemaV1 || binding.Network != network || binding.Address != normalized {
		return nil, fmt.Errorf("invalid wallet custody binding")
	}
	return &binding, nil
}

func (b *tronBackend) pathMerchantCustodyWrite(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := data.Get("tenant_id").(string)
	if !tenantIDPattern.MatchString(tenantID) || data.Get("schema_version").(string) != merchantCustodySchemaV1 {
		return logical.ErrorResponse("canonical tenant and merchant custody schema are required"), nil
	}
	encoded, err := json.Marshal(data.Get("enforced_networks"))
	if err != nil {
		return logical.ErrorResponse("invalid enforced_networks"), nil
	}
	var networks []string
	if err = json.Unmarshal(encoded, &networks); err != nil {
		return logical.ErrorResponse("invalid enforced_networks"), nil
	}
	seen := make(map[string]bool, len(networks))
	for _, network := range networks {
		if !validCustodyNetwork(network) || seen[network] {
			return logical.ErrorResponse("unsupported or duplicate custody network"), nil
		}
		seen[network] = true
	}
	slices.Sort(networks)
	b.signMu.Lock()
	defer b.signMu.Unlock()
	old, err := b.getMerchantCustodyConfig(ctx, req.Storage, tenantID)
	if err != nil {
		return logical.ErrorResponse("custody registry unavailable"), nil
	}
	revision := data.Get("revision").(int)
	if revision != old.Revision+1 {
		return logical.ErrorResponse("custody policy revision must increase by one"), nil
	}
	for _, network := range old.EnforcedNetworks {
		if !slices.Contains(networks, network) {
			return logical.ErrorResponse("merchant custody enforcement cannot be downgraded"), nil
		}
	}
	cfg := merchantCustodyConfig{SchemaVersion: merchantCustodySchemaV1, Revision: revision, EnforcedNetworks: networks}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return logical.ErrorResponse("internal error"), nil
	}
	if err = req.Storage.Put(ctx, &logical.StorageEntry{Key: merchantCustodyConfigKey(tenantID), Value: payload, SealWrap: true}); err != nil {
		return logical.ErrorResponse("failed to save merchant custody policy"), nil
	}
	return &logical.Response{Data: map[string]any{"schema_version": cfg.SchemaVersion, "revision": cfg.Revision, "enforced_networks": cfg.EnforcedNetworks}}, nil
}

func (b *tronBackend) pathMerchantCustodyRead(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := data.Get("tenant_id").(string)
	if !tenantIDPattern.MatchString(tenantID) {
		return logical.ErrorResponse("canonical tenant is required"), nil
	}
	cfg, err := b.getMerchantCustodyConfig(ctx, req.Storage, tenantID)
	if err != nil {
		return logical.ErrorResponse("custody registry unavailable"), nil
	}
	return &logical.Response{Data: map[string]any{"schema_version": merchantCustodySchemaV1, "revision": cfg.Revision, "enforced_networks": cfg.EnforcedNetworks}}, nil
}

func (b *tronBackend) pathWalletCustodyWrite(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := data.Get("tenant_id").(string)
	merchantID := data.Get("merchant_id").(string)
	network := data.Get("network").(string)
	role := data.Get("role").(string)
	if !tenantIDPattern.MatchString(tenantID) || (merchantID != "" && (!tenantIDPattern.MatchString(merchantID) || merchantID == "00000000-0000-0000-0000-000000000000")) {
		return logical.ErrorResponse("canonical tenant/merchant IDs are required"), nil
	}
	if role != "merchant_pool" && role != "liquidity_hot" && role != "network_fee" && role != "treasury" {
		return logical.ErrorResponse("unsupported custody wallet role"), nil
	}
	address, err := normalizeCustodyAddress(network, data.Get("address").(string))
	if err != nil {
		return logical.ErrorResponse("invalid custody network/address"), nil
	}
	b.signMu.Lock()
	defer b.signMu.Unlock()
	if err = requireDerivedTenantWallet(ctx, req.Storage, tenantID, network, address); err != nil {
		return logical.ErrorResponse("custody wallet must already be derived in this tenant/network"), nil
	}
	binding := merchantWalletCustody{SchemaVersion: merchantCustodySchemaV1, Network: network, Address: address, MerchantID: merchantID, Role: role}
	old, err := b.getWalletCustody(ctx, req.Storage, tenantID, network, address)
	if err != nil {
		return logical.ErrorResponse("custody registry unavailable"), nil
	}
	if old != nil {
		if *old != binding {
			return logical.ErrorResponse("wallet custody binding is immutable"), nil
		}
	} else {
		if role == "liquidity_hot" && merchantID != "" {
			if err = b.registerOwnedCollectorDestination(ctx, req.Storage, tenantID, network, address); err != nil {
				return logical.ErrorResponse("collector destination registration failed"), nil
			}
		}
		payload, err := json.Marshal(binding)
		if err != nil {
			return logical.ErrorResponse("internal error"), nil
		}
		if err = req.Storage.Put(ctx, &logical.StorageEntry{Key: merchantWalletCustodyKey(tenantID, network, address), Value: payload, SealWrap: true}); err != nil {
			return logical.ErrorResponse("failed to persist wallet custody binding"), nil
		}
	}
	return &logical.Response{Data: map[string]any{"schema_version": binding.SchemaVersion, "network": network, "address": address, "merchant_id": merchantID, "role": role}}, nil
}

func (b *tronBackend) pathWalletCustodyRead(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := data.Get("tenant_id").(string)
	if !tenantIDPattern.MatchString(tenantID) {
		return logical.ErrorResponse("canonical tenant is required"), nil
	}
	binding, err := b.getWalletCustody(ctx, req.Storage, tenantID, data.Get("network").(string), data.Get("address").(string))
	if err != nil || binding == nil {
		return logical.ErrorResponse("wallet custody binding is unavailable"), nil
	}
	return &logical.Response{Data: map[string]any{"network": binding.Network, "address": binding.Address, "merchant_id": binding.MerchantID, "role": binding.Role}}, nil
}

// Called under signMu before both signing and returning a cached signature.
// Owner binding is independent of the SQL database and never inferred from it.
func (b *tronBackend) validateMerchantCustody(ctx context.Context, storage logical.Storage, tenantID, network, purpose, source, destination string, merchantID *string) error {
	cfg, err := b.getMerchantCustodyConfig(ctx, storage, tenantID)
	if err != nil {
		return err
	}
	enforced := slices.Contains(cfg.EnforcedNetworks, network)
	from, err := b.getWalletCustody(ctx, storage, tenantID, network, source)
	if err != nil {
		return err
	}
	internal := purpose == "sweep" || purpose == "native_funding" || purpose == "activation"
	if from == nil {
		if enforced {
			return fmt.Errorf("source has no trusted merchant custody binding")
		}
		if internal {
			to, err := b.getWalletCustody(ctx, storage, tenantID, network, destination)
			if err != nil {
				return err
			}
			if to != nil && to.MerchantID != "" {
				return fmt.Errorf("unbound source cannot fund a merchant-bound destination")
			}
		}
		return nil
	}
	if from.MerchantID != "" {
		if merchantID == nil || *merchantID != from.MerchantID {
			return fmt.Errorf("authorization merchant does not own source")
		}
	} else if merchantID != nil {
		return fmt.Errorf("platform source cannot fund a merchant principal operation")
	}
	switch purpose {
	case "withdrawal":
		if from.MerchantID == "" || (from.Role != "merchant_pool" && from.Role != "liquidity_hot") {
			return fmt.Errorf("withdrawal source must be owned merchant principal")
		}
		entry, err := storage.Get(ctx, walletStorageKey(tenantID, destination))
		if err != nil {
			return err
		}
		if entry != nil {
			return fmt.Errorf("withdrawal destination must be external to managed custody")
		}
	case "treasury_withdrawal":
		if from.MerchantID != "" || (from.Role != "treasury" && (enforced || from.Role != "liquidity_hot")) {
			return fmt.Errorf("treasury cannot spend merchant principal")
		}
	case "sweep", "native_funding", "activation":
		to, err := b.getWalletCustody(ctx, storage, tenantID, network, destination)
		if err != nil {
			return err
		}
		if to == nil {
			if !enforced && from.MerchantID == "" {
				return nil
			}
			return fmt.Errorf("destination has no trusted merchant custody binding")
		}
		if from.Address == to.Address || from.MerchantID != to.MerchantID || (enforced && from.MerchantID == "") {
			return fmt.Errorf("internal transfer crosses merchant custody")
		}
		if purpose == "sweep" && (from.Role != "merchant_pool" || to.Role != "liquidity_hot") {
			return fmt.Errorf("invalid sweep custody roles")
		}
		if purpose == "native_funding" && from.Role != "network_fee" {
			return fmt.Errorf("invalid funding custody role")
		}
		if purpose == "activation" && from.Role != "network_fee" && from.Role != "liquidity_hot" {
			return fmt.Errorf("invalid activation custody role")
		}
	default:
		return fmt.Errorf("unsupported custody purpose")
	}
	return nil
}

func validCustodyNetwork(network string) bool {
	return network == "tron_nile" || network == "tron_mainnet"
}

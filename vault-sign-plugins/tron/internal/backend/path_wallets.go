package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"vault-plugin-secrets-tron/internal/json"
	"vault-plugin-secrets-tron/internal/tron"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func pathWallets(b *tronBackend) []*framework.Path {
	return []*framework.Path{
		{
			Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/wallets/derive",
			Fields: map[string]*framework.FieldSchema{
				"tenant_id": {
					Type:        framework.TypeString,
					Description: "Tenant scope from the Vault path",
				},
				"network": {
					Type:        framework.TypeString,
					Description: "TRON network code, e.g. tron_nile or tron_mainnet",
					Required:    true,
				},
				"derivation_path": {
					Type:        framework.TypeString,
					Description: "Allowed BIP-44 path m/44'/195'/{account}'/0/{index}",
					Required:    true,
				},
			},
			Operations: map[logical.Operation]framework.OperationHandler{
				logical.UpdateOperation: &framework.PathOperation{
					Callback: b.pathWalletsDerive,
				},
			},
			HelpSynopsis:    "Idempotently derives a tenant-scoped TRON address",
			HelpDescription: "Derives a deterministic TRON address from the tenant HD seed and stores the resulting Vault key handle.",
		},
	}
}

func (b *tronBackend) pathWalletsDerive(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := data.Get("tenant_id").(string)
	network := data.Get("network").(string)
	derivationPath := data.Get("derivation_path").(string)
	if tenantID == "" || network == "" || derivationPath == "" {
		return logical.ErrorResponse("tenant_id, network and derivation_path are required"), nil
	}
	if network != "tron_nile" && network != "tron_mainnet" {
		return logical.ErrorResponse("unsupported network"), nil
	}
	if err := tron.ValidateTRONHDPath(derivationPath); err != nil {
		return logical.ErrorResponse("invalid derivation_path: %v", err), nil
	}

	cfg, err := b.getAuthorizationConfig(ctx, req.Storage, tenantID)
	if err != nil {
		b.Logger().Error("failed to read authorization config", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("internal error"), nil
	}
	if cfg.SchemaVersion != tenantSigningConfigSchemaV3 {
		return logical.ErrorResponse("tenant signing config is not set"), nil
	}
	seed, err := cfg.seedBytes()
	if err != nil {
		return logical.ErrorResponse("tenant HD seed is not configured"), nil
	}

	vaultKeyID := tenantVaultKeyID(tenantID, network, derivationPath)
	derivationKey := tenantStorageKey(tenantID, "derivations", safeStoragePart(network), safeStoragePart(derivationPath))
	entry, err := req.Storage.Get(ctx, derivationKey)
	if err != nil {
		b.Logger().Error("failed to read derivation", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("internal error"), nil
	}
	if entry != nil {
		var existing map[string]string
		if err := json.Unmarshal(entry.Value, &existing); err != nil {
			b.Logger().Error("failed to unmarshal derivation", "error", err, "tenant_id", tenantID)
			return logical.ErrorResponse("internal error"), nil
		}
		if existing["address"] != "" {
			return &logical.Response{Data: map[string]any{
				"address":         existing["address"],
				"vault_key_id":    existing["vault_key_id"],
				"derivation_path": derivationPath,
			}}, nil
		}
	}

	wallet, err := tron.DeriveWalletFromSeed(seed, derivationPath)
	if err != nil {
		b.Logger().Error("failed to derive TRON wallet", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("failed to derive wallet"), nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	walletData := map[string]string{
		"private_key":     wallet.PrivateKey,
		"public_key":      wallet.PublicKey,
		"vault_key_id":    vaultKeyID,
		"derivation_path": derivationPath,
		"network":         network,
		"tenant_id":       tenantID,
		"created_at":      now,
	}
	walletJSON, err := json.Marshal(walletData)
	if err != nil {
		b.Logger().Error("failed to marshal wallet data", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("internal error"), nil
	}
	if err := req.Storage.Put(ctx, &logical.StorageEntry{
		Key:      tenantStorageKey(tenantID, "wallets", safeStoragePart(wallet.Address)),
		Value:    walletJSON,
		SealWrap: true,
	}); err != nil {
		b.Logger().Error("failed to save wallet", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("failed to save wallet"), nil
	}

	derivationJSON, err := json.Marshal(map[string]string{
		"address":         wallet.Address,
		"vault_key_id":    vaultKeyID,
		"derivation_path": derivationPath,
		"network":         network,
		"tenant_id":       tenantID,
		"created_at":      now,
	})
	if err != nil {
		b.Logger().Error("failed to marshal derivation", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("internal error"), nil
	}
	if err := req.Storage.Put(ctx, &logical.StorageEntry{
		Key:      derivationKey,
		Value:    derivationJSON,
		SealWrap: true,
	}); err != nil {
		b.Logger().Error("failed to save derivation", "error", err, "tenant_id", tenantID)
		return logical.ErrorResponse("failed to save derivation"), nil
	}

	b.Logger().Info("successfully derived and stored TRON wallet", "address", wallet.Address, "tenant_id", tenantID)
	return &logical.Response{Data: map[string]any{
		"address":         wallet.Address,
		"vault_key_id":    vaultKeyID,
		"derivation_path": derivationPath,
	}}, nil
}

func authorizationConfigKey(tenantID string) string {
	return tenantStorageKey(tenantID, "config", "authorization")
}

func sweepDestinationsConfigKey(tenantID string) string {
	return tenantStorageKey(tenantID, "config", "sweep-destinations")
}

func walletStorageKey(tenantID, address string) string {
	return tenantStorageKey(tenantID, "wallets", safeStoragePart(address))
}

func signatureStorageKey(tenantID, authorizationID string) string {
	return tenantStorageKey(tenantID, "signatures", safeStoragePart(authorizationID))
}

func intentConsumptionStorageKey(tenantID, intentDigest string) string {
	return tenantStorageKey(tenantID, "consumed-intents", safeStoragePart(intentDigest))
}

func businessBasisStorageKey(tenantID, purpose, basisType, basisID string) string {
	return tenantStorageKey(tenantID, "consumed-basis", safeStoragePart(purpose), safeStoragePart(basisType), safeStoragePart(basisID))
}

func tenantVaultKeyID(tenantID, network, derivationPath string) string {
	return tenantID + "/" + network + "/" + derivationPath
}

func tenantStorageKey(tenantID string, parts ...string) string {
	segments := []string{"tenants", safeStoragePart(tenantID)}
	segments = append(segments, parts...)
	return strings.Join(segments, "/")
}

func safeStoragePart(s string) string {
	normalized := strings.ToLower(strings.TrimSpace(s))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"vault-plugin-secrets-evm/internal/evm"
	"vault-plugin-secrets-evm/internal/json"

	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

func pathWallets(b *evmBackend) []*framework.Path {
	return []*framework.Path{{
		Pattern: "tenants/" + framework.GenericNameRegex("tenant_id") + "/wallets/derive",
		Fields: map[string]*framework.FieldSchema{
			"tenant_id":       {Type: framework.TypeString},
			"chain_id":        {Type: framework.TypeString, Required: true},
			"derivation_path": {Type: framework.TypeString, Required: true},
		},
		Operations: map[logical.Operation]framework.OperationHandler{
			logical.UpdateOperation: &framework.PathOperation{Callback: b.pathWalletsDerive},
		},
		HelpSynopsis: "Idempotently derives a tenant-scoped EVM address",
	}}
}

func (b *evmBackend) pathWalletsDerive(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	tenantID := strings.TrimSpace(data.Get("tenant_id").(string))
	chainID := strings.TrimSpace(data.Get("chain_id").(string))
	derivationPath := data.Get("derivation_path").(string)
	if !tenantIDPattern.MatchString(tenantID) || chainID != evm.SepoliaChainID {
		return logical.ErrorResponse("unsupported tenant or chain_id"), nil
	}
	if err := evm.ValidateHDPath(derivationPath); err != nil {
		return logical.ErrorResponse("invalid derivation_path: %v", err), nil
	}
	cfg, err := b.getAuthorizationConfig(ctx, req.Storage, tenantID)
	if err != nil || cfg.SchemaVersion != tenantSigningConfigSchemaV1 || len(cfg.AllowedChainIDs) != 1 || cfg.AllowedChainIDs[0] != chainID {
		return logical.ErrorResponse("tenant signing configuration is unavailable for chain_id"), nil
	}
	seed, err := cfg.seedBytes()
	if err != nil {
		return logical.ErrorResponse("tenant EVM HD seed is unavailable"), nil
	}
	vaultKeyID := tenantVaultKeyID(tenantID, chainID, derivationPath)
	derivationKey := tenantStorageKey(tenantID, "derivations", safeStoragePart(chainID), safeStoragePart(derivationPath))
	if entry, getErr := req.Storage.Get(ctx, derivationKey); getErr != nil {
		return logical.ErrorResponse("internal error"), nil
	} else if entry != nil {
		var existing map[string]string
		if json.Unmarshal(entry.Value, &existing) == nil && existing["address"] != "" {
			return &logical.Response{Data: map[string]any{"address": existing["address"], "vault_key_id": existing["vault_key_id"], "derivation_path": derivationPath}}, nil
		}
	}
	wallet, err := evm.DeriveWalletFromSeed(seed, derivationPath)
	if err != nil {
		return logical.ErrorResponse("failed to derive EVM wallet"), nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	walletData := map[string]string{"address": wallet.Address, "private_key": wallet.PrivateKey, "public_key": wallet.PublicKey,
		"vault_key_id": vaultKeyID, "derivation_path": derivationPath, "chain_id": chainID, "tenant_id": tenantID, "created_at": now}
	encoded, _ := json.Marshal(walletData)
	if err := req.Storage.Put(ctx, &logical.StorageEntry{Key: walletStorageKey(tenantID, wallet.Address), Value: encoded, SealWrap: true}); err != nil {
		return logical.ErrorResponse("failed to persist EVM wallet"), nil
	}
	derivationData, _ := json.Marshal(map[string]string{"address": wallet.Address, "vault_key_id": vaultKeyID,
		"derivation_path": derivationPath, "chain_id": chainID, "tenant_id": tenantID, "created_at": now})
	if err := req.Storage.Put(ctx, &logical.StorageEntry{Key: derivationKey, Value: derivationData, SealWrap: true}); err != nil {
		return logical.ErrorResponse("failed to persist EVM derivation"), nil
	}
	return &logical.Response{Data: map[string]any{"address": wallet.Address, "vault_key_id": vaultKeyID, "derivation_path": derivationPath}}, nil
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
func businessGenerationStorageKey(tenantID, purpose, basisType, basisID string, attempt, generation int) string {
	return tenantStorageKey(tenantID, "business-generations", safeStoragePart(purpose), safeStoragePart(basisType), safeStoragePart(basisID), safeStoragePart(strconv.Itoa(attempt)), safeStoragePart(strconv.Itoa(generation)))
}
func tenantVaultKeyID(tenantID, chainID, derivationPath string) string {
	return tenantID + "/" + chainID + "/" + derivationPath
}
func tenantStorageKey(tenantID string, parts ...string) string {
	segments := []string{"tenants", safeStoragePart(tenantID)}
	return strings.Join(append(segments, parts...), "/")
}
func safeStoragePart(value string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(value))))
	return hex.EncodeToString(sum[:])
}

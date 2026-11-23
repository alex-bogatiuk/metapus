package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	walletcatalog "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

type vaultClient struct {
	baseURL string
	http    *http.Client
}

type vaultInitMaterial struct {
	UnsealKey string `json:"unseal_key"`
	RootToken string `json:"root_token"`
}

type derivedWallet struct {
	Code           string                   `json:"code"`
	Tier           walletcatalog.WalletTier `json:"tier"`
	Role           walletcatalog.WalletRole `json:"wallet_role"`
	Address        string                   `json:"address"`
	DerivationPath string                   `json:"derivation_path"`
	VaultKeyID     string                   `json:"vault_key_id"`
}

type derivedWalletSet struct {
	TenantID         string          `json:"tenant_id"`
	Network          string          `json:"network"`
	AuthorizationKID string          `json:"authorization_kid"`
	Wallets          []derivedWallet `json:"wallets"`
}

func runVaultInit(args []string) error {
	fs := flag.NewFlagSet("vault-init", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	vaultAddr := fs.String("vault-addr", "https://127.0.0.1:8200", "Vault API address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required")
	}
	client, absState, err := vaultClientForState(*stateDir, *vaultAddr)
	if err != nil {
		return err
	}
	var status struct {
		Initialized bool `json:"initialized"`
	}
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		lastErr = client.request(context.Background(), http.MethodGet, "sys/init", "", nil, nil, &status)
		if lastErr == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if lastErr != nil {
		return fmt.Errorf("wait for vault readiness: %w", lastErr)
	}
	if status.Initialized {
		material, err := readVaultInitMaterial(absState)
		if err != nil {
			return fmt.Errorf("vault is already initialized, but failed to read operator material: %w; use vault-unseal with the existing state", err)
		}
		if err := unsealVault(client, material.UnsealKey); err != nil {
			return fmt.Errorf("unseal already-initialized vault: %w", err)
		}
		fmt.Println("Vault is already initialized; unsealed successfully using existing operator material.")
		return nil
	}
	var initialized struct {
		KeysBase64 []string `json:"keys_base64"`
		RootToken  string   `json:"root_token"`
	}
	if err := client.request(context.Background(), http.MethodPost, "sys/init", "", nil, map[string]any{
		"secret_shares":    1,
		"secret_threshold": 1,
	}, &initialized); err != nil {
		return err
	}
	if len(initialized.KeysBase64) != 1 || initialized.RootToken == "" {
		return errors.New("vault init returned incomplete operator material")
	}
	material := vaultInitMaterial{UnsealKey: initialized.KeysBase64[0], RootToken: initialized.RootToken}
	raw, err := json.Marshal(material)
	if err != nil {
		return err
	}
	if err := writeExclusive(filepath.Join(absState, secretDirName, "vault_init.json"), raw, 0o600); err != nil {
		return err
	}
	if err := unsealVault(client, material.UnsealKey); err != nil {
		return err
	}
	fmt.Println("Vault initialized with one test-only unseal share and manually unsealed; operator material was stored outside the repository.")
	return nil
}

func runVaultUnseal(args []string) error {
	fs := flag.NewFlagSet("vault-unseal", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	vaultAddr := fs.String("vault-addr", "https://127.0.0.1:8200", "Vault API address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required")
	}
	client, absState, err := vaultClientForState(*stateDir, *vaultAddr)
	if err != nil {
		return err
	}
	material, err := readVaultInitMaterial(absState)
	if err != nil {
		return err
	}
	if err := unsealVault(client, material.UnsealKey); err != nil {
		return err
	}
	fmt.Println("Vault manually unsealed from operator material outside the repository.")
	return nil
}

func runVaultBootstrap(args []string) error {
	fs := flag.NewFlagSet("vault-bootstrap", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	tenantID := fs.String("tenant-id", "", "tenant UUID")
	poolAccount := fs.Int("pool-account", -1, "fresh BIP-44 pool account")
	pluginSHA256 := fs.String("plugin-sha256", "", "SHA-256 of the in-container custody plugin for --family")
	evmPluginSHA256 := fs.String("evm-plugin-sha256", "", "SHA-256 of the in-container EVM plugin; required when --family=both")
	family := fs.String("family", "both", "custody family: tron, evm, or both")
	wrapSecretID := fs.Bool("wrap-secret-id", true, "issue a one-use Vault Proxy bootstrap token")
	proxyCIDR := fs.String("proxy-cidr", "172.30.251.10/32", "exact Vault Proxy source CIDR")
	vaultAddr := fs.String("vault-addr", "https://127.0.0.1:8200", "Vault API address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" || *tenantID == "" || *pluginSHA256 == "" || *poolAccount < 0 {
		return errors.New("--state-dir, --tenant-id, --pool-account, and --plugin-sha256 are required")
	}
	if _, err := id.Parse(*tenantID); err != nil {
		return fmt.Errorf("invalid tenant UUID: %w", err)
	}
	selectedFamily := strings.ToLower(strings.TrimSpace(*family))
	if selectedFamily == "both" && strings.TrimSpace(*evmPluginSHA256) == "" {
		return errors.New("--evm-plugin-sha256 is required when --family=both")
	}
	proxyIP, parsedCIDR, err := net.ParseCIDR(strings.TrimSpace(*proxyCIDR))
	if err != nil || proxyIP == nil || parsedCIDR == nil {
		return errors.New("--proxy-cidr must be a valid CIDR")
	}
	ones, bits := parsedCIDR.Mask.Size()
	if bits != 32 || ones != 32 || !proxyIP.IsPrivate() {
		return errors.New("--proxy-cidr must be one private IPv4 /32")
	}
	client, absState, err := vaultClientForState(*stateDir, *vaultAddr)
	if err != nil {
		return err
	}
	material, err := readVaultInitMaterial(absState)
	if err != nil {
		return err
	}
	families := []struct {
		name   string
		sha256 string
		wrap   bool
	}{{name: selectedFamily, sha256: *pluginSHA256, wrap: *wrapSecretID}}
	if selectedFamily == "both" {
		families = []struct {
			name   string
			sha256 string
			wrap   bool
		}{
			{name: "tron", sha256: *pluginSHA256, wrap: false},
			{name: "evm", sha256: *evmPluginSHA256, wrap: *wrapSecretID},
		}
	}
	var last derivedWalletSet
	for _, item := range families {
		wallets, bootErr := bootstrapVaultTenant(context.Background(), client, absState, material.RootToken, *tenantID, *poolAccount, item.sha256, parsedCIDR.String(), item.name, item.wrap)
		if bootErr != nil {
			return bootErr
		}
		last = wallets
		if err := printDerivedWalletSummary(item.name, wallets); err != nil {
			return err
		}
	}
	_ = last
	return nil
}

func printDerivedWalletSummary(family string, wallets derivedWalletSet) error {
	liquidityHot, err := derivedWalletByRole(wallets.Wallets, walletcatalog.WalletRoleLiquidityHot)
	if err != nil {
		return err
	}
	networkFee, err := derivedWalletByRole(wallets.Wallets, walletcatalog.WalletRoleNetworkFee)
	if err != nil {
		return err
	}
	poolWallets := derivedWalletsByRole(wallets.Wallets, walletcatalog.WalletRoleMerchantPool)
	if len(poolWallets) != 2 {
		return fmt.Errorf("derived wallet set contains %d merchant pool wallets, want 2", len(poolWallets))
	}
	fmt.Printf("Vault Transit, %s plugin, AppRole, worker policy, and one-time Proxy bootstrap are ready.\nLiquidity hot wallet: %s\nNetwork fee wallet: %s\nPool wallet 1: %s\nPool wallet 2: %s\n", strings.ToUpper(strings.TrimSpace(family)), liquidityHot.Address, networkFee.Address, poolWallets[0].Address, poolWallets[1].Address)
	return nil
}

func runVaultWrapSecretID(args []string) error {
	fs := flag.NewFlagSet("vault-wrap-secret-id", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	vaultAddr := fs.String("vault-addr", "https://127.0.0.1:8200", "Vault API address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required")
	}
	client, absState, err := vaultClientForState(*stateDir, *vaultAddr)
	if err != nil {
		return err
	}
	material, err := readVaultInitMaterial(absState)
	if err != nil {
		return err
	}
	if err := writeWrappedSecretID(context.Background(), client, absState, material.RootToken); err != nil {
		return err
	}
	fmt.Println("A new 120-second wrapped one-use AppRole SecretID was stored outside the repository.")
	return nil
}

func bootstrapVaultTenant(ctx context.Context, client *vaultClient, stateDir, rootToken, tenantID string, poolAccount int, pluginSHA256, proxyCIDR, family string, wrapSecretID bool) (derivedWalletSet, error) {
	family = strings.ToLower(strings.TrimSpace(family))
	pluginName, mount, network, chainID := "vault-plugin-secrets-tron", "tron", "tron_nile", ""
	if family == "evm" {
		pluginName, mount, network, chainID = "vault-plugin-secrets-evm", "evm", "ethereum_sepolia", "11155111"
	} else if family != "tron" {
		return derivedWalletSet{}, fmt.Errorf("unsupported Vault custody family %q", family)
	}
	if err := client.request(ctx, http.MethodPost, "sys/plugins/catalog/secret/"+pluginName, rootToken, nil, map[string]any{
		"sha256":  strings.TrimSpace(pluginSHA256),
		"command": pluginName,
	}, nil); err != nil {
		return derivedWalletSet{}, fmt.Errorf("register %s plugin: %w", strings.ToUpper(family), err)
	}
	if err := client.ensureMount(ctx, rootToken, "transit", map[string]any{"type": "transit"}); err != nil {
		return derivedWalletSet{}, fmt.Errorf("enable Transit: %w", err)
	}
	if err := client.ensureMount(ctx, rootToken, "secret", map[string]any{
		"type":    "kv",
		"options": map[string]string{"version": "2"},
	}); err != nil {
		return derivedWalletSet{}, fmt.Errorf("enable webhook-secret KV v2: %w", err)
	}
	if err := client.ensureMount(ctx, rootToken, mount, map[string]any{
		"type":        "plugin",
		"plugin_name": pluginName,
	}); err != nil {
		return derivedWalletSet{}, fmt.Errorf("enable %s plugin: %w", strings.ToUpper(family), err)
	}
	if err := client.ensureAuth(ctx, rootToken, "approle", map[string]any{"type": "approle"}); err != nil {
		return derivedWalletSet{}, fmt.Errorf("enable AppRole: %w", err)
	}

	transitKey := "metapus-signing-auth-" + tenantID
	if err := client.ensureTransitEd25519Key(ctx, rootToken, transitKey); err != nil {
		return derivedWalletSet{}, err
	}
	var transitMetadata struct {
		Data struct {
			Keys map[string]struct {
				PublicKey string `json:"public_key"`
			} `json:"keys"`
			Exportable           bool `json:"exportable"`
			DeletionAllowed      bool `json:"deletion_allowed"`
			AllowPlaintextBackup bool `json:"allow_plaintext_backup"`
		} `json:"data"`
	}
	if err := client.request(ctx, http.MethodGet, "transit/keys/"+transitKey, rootToken, nil, nil, &transitMetadata); err != nil {
		return derivedWalletSet{}, fmt.Errorf("read Transit metadata: %w", err)
	}
	if transitMetadata.Data.Exportable || transitMetadata.Data.DeletionAllowed || transitMetadata.Data.AllowPlaintextBackup {
		return derivedWalletSet{}, errors.New("transit authorization key metadata violates non-exportable/deletion-disabled policy")
	}
	versions := make([]int, 0, len(transitMetadata.Data.Keys))
	for rawVersion := range transitMetadata.Data.Keys {
		version, convErr := strconv.Atoi(rawVersion)
		if convErr != nil {
			return derivedWalletSet{}, fmt.Errorf("invalid Transit key version %q", rawVersion)
		}
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return derivedWalletSet{}, errors.New("transit key has no public versions")
	}
	sort.Ints(versions)
	version := versions[len(versions)-1]
	publicKey := transitMetadata.Data.Keys[strconv.Itoa(version)].PublicKey
	kid := fmt.Sprintf("transit/%s/v%d", transitKey, version)
	publicKeyFile := filepath.Join(stateDir, "vault", "transit-public-key")
	if err := writeAtomic(publicKeyFile, []byte(publicKey), 0o600); err != nil {
		return derivedWalletSet{}, err
	}
	approvalPublicKeys, err := bootstrapApprovalTransitKeys(ctx, client, rootToken, tenantID)
	if err != nil {
		return derivedWalletSet{}, err
	}
	if err := updateEnvFile(filepath.Join(stateDir, envFileName), map[string]string{
		"CRYPTO_INTENT_SIGNING_PUBLIC_KEY":          approvalPublicKeys["intent"],
		"CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY": approvalPublicKeys["risk_assessment"],
		"CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY": approvalPublicKeys["policy_decision"],
		"CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY":  approvalPublicKeys["admin_approval"],
		"CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY":   approvalPublicKeys["risk_approval"],
	}); err != nil {
		return derivedWalletSet{}, err
	}
	if err := createVaultTenantConfigForFamily(stateDir, tenantID, kid, publicKey, approvalPublicKeys, family); err != nil {
		return derivedWalletSet{}, err
	}
	configRaw, err := os.ReadFile(filepath.Join(stateDir, secretDirName, "vault_tenant_signing_config.json"))
	if err != nil {
		return derivedWalletSet{}, err
	}
	var config any
	if err := json.Unmarshal(configRaw, &config); err != nil {
		return derivedWalletSet{}, err
	}
	if err := client.request(ctx, http.MethodPost, mount+"/tenants/"+tenantID+"/config/authorization", rootToken, nil, config, nil); err != nil {
		return derivedWalletSet{}, fmt.Errorf("write tenant authorization config: %w", err)
	}

	walletSpecs := initialNileWalletSpecs(poolAccount)
	if family == "evm" {
		walletSpecs = initialSepoliaWalletSpecs(poolAccount)
	}
	for i := range walletSpecs {
		var response struct {
			Data struct {
				Address        string `json:"address"`
				VaultKeyID     string `json:"vault_key_id"`
				DerivationPath string `json:"derivation_path"`
			} `json:"data"`
		}
		deriveRequest := map[string]any{"derivation_path": walletSpecs[i].DerivationPath}
		if family == "evm" {
			deriveRequest["chain_id"] = chainID
		} else {
			deriveRequest["network"] = network
		}
		if err := client.request(ctx, http.MethodPost, mount+"/tenants/"+tenantID+"/wallets/derive", rootToken, nil, deriveRequest, &response); err != nil {
			return derivedWalletSet{}, fmt.Errorf("derive %s: %w", walletSpecs[i].Code, err)
		}
		if response.Data.Address == "" || response.Data.VaultKeyID == "" || response.Data.DerivationPath != walletSpecs[i].DerivationPath {
			return derivedWalletSet{}, fmt.Errorf("vault returned incomplete identity for %s", walletSpecs[i].Code)
		}
		walletSpecs[i].Address = response.Data.Address
		walletSpecs[i].VaultKeyID = response.Data.VaultKeyID
	}
	liquidityHot, err := derivedWalletByRole(walletSpecs, walletcatalog.WalletRoleLiquidityHot)
	if err != nil {
		return derivedWalletSet{}, err
	}
	sweepSchema, sweepNetwork := "tron_sweep_destinations.v1", network
	if family == "evm" {
		sweepSchema, sweepNetwork = "evm_sweep_destinations.v1", chainID
	}
	sweepPolicy := map[string]any{
		"schema_version": sweepSchema,
		"version":        time.Now().UTC().Format("2006-01-02.150405"),
		"destinations": map[string][]string{
			sweepNetwork: {liquidityHot.Address},
		},
	}
	if err := client.request(ctx, http.MethodPost, mount+"/tenants/"+tenantID+"/config/sweep-destinations", rootToken, nil, sweepPolicy, nil); err != nil {
		return derivedWalletSet{}, fmt.Errorf("write sweep destination policy: %w", err)
	}

	walletSet := derivedWalletSet{
		TenantID:         tenantID,
		Network:          network,
		AuthorizationKID: kid,
		Wallets:          walletSpecs,
	}
	walletRaw, err := json.Marshal(walletSet)
	if err != nil {
		return derivedWalletSet{}, err
	}
	familyWalletFile := "wallets-tron.json"
	if family == "evm" {
		familyWalletFile = "wallets-evm.json"
	}
	if err := writeAtomic(filepath.Join(stateDir, familyWalletFile), walletRaw, 0o600); err != nil {
		return derivedWalletSet{}, err
	}
	if err := writeAtomic(filepath.Join(stateDir, "wallets.json"), walletRaw, 0o600); err != nil {
		return derivedWalletSet{}, err
	}

	policy := workerVaultPolicy(tenantID)
	if err := client.request(ctx, http.MethodPost, "sys/policies/acl/metapus-worker", rootToken, nil, map[string]any{"policy": policy}, nil); err != nil {
		return derivedWalletSet{}, fmt.Errorf("write worker policy: %w", err)
	}
	if err := writeAtomic(filepath.Join(stateDir, "vault", "worker-policy.hcl"), []byte(policy), 0o400); err != nil {
		return derivedWalletSet{}, err
	}
	if err := client.request(ctx, http.MethodPost, "auth/approle/role/metapus-worker", rootToken, nil, map[string]any{
		"token_policies":          []string{"metapus-worker"},
		"token_no_default_policy": true,
		"token_type":              "service",
		"token_period":            "10m",
		"token_num_uses":          0,
		"secret_id_ttl":           "5m",
		"secret_id_num_uses":      1,
		"secret_id_bound_cidrs":   []string{proxyCIDR},
		"token_bound_cidrs":       []string{proxyCIDR},
	}, nil); err != nil {
		return derivedWalletSet{}, fmt.Errorf("configure worker AppRole: %w", err)
	}
	var roleIDResponse struct {
		Data struct {
			RoleID string `json:"role_id"`
		} `json:"data"`
	}
	if err := client.request(ctx, http.MethodGet, "auth/approle/role/metapus-worker/role-id", rootToken, nil, nil, &roleIDResponse); err != nil {
		return derivedWalletSet{}, fmt.Errorf("read AppRole RoleID: %w", err)
	}
	if roleIDResponse.Data.RoleID == "" {
		return derivedWalletSet{}, errors.New("vault returned an empty AppRole RoleID")
	}
	bootstrapDir := filepath.Join(stateDir, "vault", "bootstrap")
	if err := writeAtomic(filepath.Join(bootstrapDir, "role-id"), []byte(roleIDResponse.Data.RoleID), 0o444); err != nil {
		return derivedWalletSet{}, err
	}
	if wrapSecretID {
		if err := writeWrappedSecretID(ctx, client, stateDir, rootToken); err != nil {
			return derivedWalletSet{}, err
		}
	}
	return walletSet, nil
}

func bootstrapApprovalTransitKeys(ctx context.Context, client *vaultClient, rootToken, tenantID string) (map[string]string, error) {
	roles := map[string]string{
		"intent":          "intent",
		"risk-assessment": "risk_assessment",
		"policy-decision": "policy_decision",
		"admin-approval":  "admin_approval",
		"risk-approval":   "risk_approval",
	}
	publicKeys := make(map[string]string, len(roles))
	for transitRole, configRole := range roles {
		keyName := "metapus-approval-" + transitRole + "-" + tenantID
		if err := client.ensureTransitEd25519Key(ctx, rootToken, keyName); err != nil {
			return nil, fmt.Errorf("%s approval Transit key: %w", transitRole, err)
		}
		var metadata struct {
			Data struct {
				Type                 string `json:"type"`
				LatestVersion        int    `json:"latest_version"`
				Exportable           bool   `json:"exportable"`
				DeletionAllowed      bool   `json:"deletion_allowed"`
				AllowPlaintextBackup bool   `json:"allow_plaintext_backup"`
				Keys                 map[string]struct {
					PublicKey string `json:"public_key"`
				} `json:"keys"`
			} `json:"data"`
		}
		if err := client.request(ctx, http.MethodGet, "transit/keys/"+keyName, rootToken, nil, nil, &metadata); err != nil {
			return nil, fmt.Errorf("read %s approval Transit metadata: %w", transitRole, err)
		}
		current, ok := metadata.Data.Keys[strconv.Itoa(metadata.Data.LatestVersion)]
		if metadata.Data.Type != "ed25519" || metadata.Data.LatestVersion < 1 || !ok || strings.TrimSpace(current.PublicKey) == "" ||
			metadata.Data.Exportable || metadata.Data.DeletionAllowed || metadata.Data.AllowPlaintextBackup {
			return nil, fmt.Errorf("%s approval Transit key violates key custody policy", transitRole)
		}
		publicKeys[configRole] = strings.TrimSpace(current.PublicKey)
	}
	return publicKeys, nil
}

func initialNileWalletSpecs(poolAccount int) []derivedWallet {
	return []derivedWallet{
		{
			Code:           "HOT-TRON-001",
			Tier:           walletcatalog.WalletTierHot,
			Role:           walletcatalog.WalletRoleLiquidityHot,
			DerivationPath: "m/44'/195'/0'/0/0",
		},
		{
			Code:           "FEE-TRON-001",
			Tier:           walletcatalog.WalletTierHot,
			Role:           walletcatalog.WalletRoleNetworkFee,
			DerivationPath: "m/44'/195'/0'/0/1",
		},
		{
			Code:           fmt.Sprintf("POOL-TRON-A%03d-001", poolAccount),
			Tier:           walletcatalog.WalletTierPool,
			Role:           walletcatalog.WalletRoleMerchantPool,
			DerivationPath: fmt.Sprintf("m/44'/195'/%d'/0/1", poolAccount),
		},
		{
			Code:           fmt.Sprintf("POOL-TRON-A%03d-002", poolAccount),
			Tier:           walletcatalog.WalletTierPool,
			Role:           walletcatalog.WalletRoleMerchantPool,
			DerivationPath: fmt.Sprintf("m/44'/195'/%d'/0/2", poolAccount),
		},
	}
}

func initialSepoliaWalletSpecs(poolAccount int) []derivedWallet {
	return []derivedWallet{
		{Code: "HOT-EVM-001", Tier: walletcatalog.WalletTierHot, Role: walletcatalog.WalletRoleLiquidityHot, DerivationPath: "m/44'/60'/0'/0/0"},
		{Code: "FEE-EVM-001", Tier: walletcatalog.WalletTierHot, Role: walletcatalog.WalletRoleNetworkFee, DerivationPath: "m/44'/60'/0'/0/1"},
		{Code: fmt.Sprintf("POOL-EVM-A%03d-001", poolAccount), Tier: walletcatalog.WalletTierPool, Role: walletcatalog.WalletRoleMerchantPool, DerivationPath: fmt.Sprintf("m/44'/60'/%d'/0/1", poolAccount)},
		{Code: fmt.Sprintf("POOL-EVM-A%03d-002", poolAccount), Tier: walletcatalog.WalletTierPool, Role: walletcatalog.WalletRoleMerchantPool, DerivationPath: fmt.Sprintf("m/44'/60'/%d'/0/2", poolAccount)},
	}
}

func derivedWalletByRole(wallets []derivedWallet, role walletcatalog.WalletRole) (derivedWallet, error) {
	matches := derivedWalletsByRole(wallets, role)
	if len(matches) != 1 {
		return derivedWallet{}, fmt.Errorf("derived wallet set contains %d wallets with role %q, want 1", len(matches), role)
	}
	return matches[0], nil
}

func derivedWalletsByRole(wallets []derivedWallet, role walletcatalog.WalletRole) []derivedWallet {
	matches := make([]derivedWallet, 0, len(wallets))
	for _, candidate := range wallets {
		if candidate.Role == role {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func writeWrappedSecretID(ctx context.Context, client *vaultClient, stateDir, rootToken string) error {
	var wrappedResponse struct {
		WrapInfo struct {
			Token        string `json:"token"`
			CreationPath string `json:"creation_path"`
		} `json:"wrap_info"`
	}
	if err := client.request(ctx, http.MethodPost, "auth/approle/role/metapus-worker/secret-id", rootToken, map[string]string{
		"X-Vault-Wrap-TTL": "120s",
	}, map[string]any{}, &wrappedResponse); err != nil {
		return fmt.Errorf("create wrapped AppRole SecretID: %w", err)
	}
	expectedCreationPath := "auth/approle/role/metapus-worker/secret-id"
	if wrappedResponse.WrapInfo.Token == "" || wrappedResponse.WrapInfo.CreationPath != expectedCreationPath {
		return errors.New("vault returned an invalid wrapped SecretID response")
	}
	bootstrapDir := filepath.Join(stateDir, "vault", "bootstrap")
	// Start read-only for every identity. On Linux the permission-init service
	// chowns this to the Vault UID and tightens it to 0400. Docker Desktop bind
	// mounts cannot chown/chmod Windows files, so 0444 is required for the
	// non-root Vault UID; the host directory ACL remains user+SYSTEM only.
	if err := writeAtomic(filepath.Join(bootstrapDir, "wrapped-secret-id"), []byte(wrappedResponse.WrapInfo.Token), 0o444); err != nil {
		return err
	}
	return nil
}

func workerVaultPolicy(tenantID string) string {
	return fmt.Sprintf(`path "auth/token/renew-self" {
  capabilities = ["update"]
}

path "transit/keys/metapus-signing-auth-%[1]s" {
  capabilities = ["read"]
}

path "transit/sign/metapus-signing-auth-%[1]s" {
  capabilities = ["update"]
}

path "transit/keys/metapus-approval-intent-%[1]s" {
  capabilities = ["read"]
}
path "transit/sign/metapus-approval-intent-%[1]s" {
  capabilities = ["update"]
}
path "transit/keys/metapus-approval-risk-assessment-%[1]s" {
  capabilities = ["read"]
}
path "transit/sign/metapus-approval-risk-assessment-%[1]s" {
  capabilities = ["update"]
}
path "transit/keys/metapus-approval-policy-decision-%[1]s" {
  capabilities = ["read"]
}
path "transit/sign/metapus-approval-policy-decision-%[1]s" {
  capabilities = ["update"]
}
path "transit/keys/metapus-approval-admin-approval-%[1]s" {
  capabilities = ["read"]
}
path "transit/sign/metapus-approval-admin-approval-%[1]s" {
  capabilities = ["update"]
}
path "transit/keys/metapus-approval-risk-approval-%[1]s" {
  capabilities = ["read"]
}
path "transit/sign/metapus-approval-risk-approval-%[1]s" {
  capabilities = ["update"]
}

path "secret/data/merchant-webhooks/*" {
  capabilities = ["create", "read", "update"]
}

path "secret/metadata/merchant-webhooks/*" {
  capabilities = ["delete"]
}

path "tron/tenants/%[1]s/config/readiness" {
  capabilities = ["read"]
}

path "tron/tenants/%[1]s/wallets/derive" {
  capabilities = ["update"]
}

path "tron/tenants/%[1]s/tx/sign" {
  capabilities = ["update"]
}

path "evm/tenants/%[1]s/config/readiness" {
  capabilities = ["read"]
}

path "evm/tenants/%[1]s/wallets/derive" {
  capabilities = ["update"]
}

path "evm/tenants/%[1]s/tx/sign" {
  capabilities = ["update"]
}
`, tenantID)
}

func runVaultWorkerPolicy(args []string) error {
	fs := flag.NewFlagSet("vault-worker-policy", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	tenantID := fs.String("tenant-id", "", "tenant UUID")
	vaultAddr := fs.String("vault-addr", "https://127.0.0.1:8200", "Vault API address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" || *tenantID == "" {
		return errors.New("--state-dir and --tenant-id are required")
	}
	parsedTenantID, err := id.Parse(*tenantID)
	if err != nil || parsedTenantID.String() != strings.ToLower(strings.TrimSpace(*tenantID)) {
		return errors.New("--tenant-id must be a canonical UUID")
	}
	client, absState, err := vaultClientForState(*stateDir, *vaultAddr)
	if err != nil {
		return err
	}
	material, err := readVaultInitMaterial(absState)
	if err != nil {
		return err
	}
	policy := workerVaultPolicy(parsedTenantID.String())
	if err := client.request(
		context.Background(),
		http.MethodPost,
		"sys/policies/acl/metapus-worker",
		material.RootToken,
		nil,
		map[string]any{"policy": policy},
		nil,
	); err != nil {
		return fmt.Errorf("write worker policy: %w", err)
	}
	if err := writeAtomic(
		filepath.Join(absState, "vault", "worker-policy.hcl"),
		[]byte(policy),
		0o400,
	); err != nil {
		return err
	}
	fmt.Println("Tenant-scoped Vault worker policy refreshed.")
	return nil
}

func vaultClientForState(stateDir, vaultAddr string) (*vaultClient, string, error) {
	absState, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, "", err
	}
	caPEM, err := os.ReadFile(filepath.Join(absState, "vault", "tls", "ca.crt"))
	if err != nil {
		return nil, "", fmt.Errorf("read Vault CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, "", errors.New("vault CA file contains no certificates")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return &vaultClient{
		baseURL: strings.TrimRight(vaultAddr, "/"),
		http:    &http.Client{Transport: transport, Timeout: 30 * time.Second},
	}, absState, nil
}

func (c *vaultClient) hasMount(ctx context.Context, token, mount string) (bool, error) {
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "sys/mounts", token, nil, nil, &out); err != nil {
		return false, err
	}
	_, ok := out.Data[strings.Trim(mount, "/")+"/"]
	return ok, nil
}

func (c *vaultClient) ensureMount(ctx context.Context, token, mount string, body map[string]any) error {
	exists, err := c.hasMount(ctx, token, mount)
	if err != nil || exists {
		return err
	}
	return c.request(ctx, http.MethodPost, "sys/mounts/"+strings.Trim(mount, "/"), token, nil, body, nil)
}

func (c *vaultClient) hasAuth(ctx context.Context, token, method string) (bool, error) {
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "sys/auth", token, nil, nil, &out); err != nil {
		return false, err
	}
	_, ok := out.Data[strings.Trim(method, "/")+"/"]
	return ok, nil
}

func (c *vaultClient) ensureAuth(ctx context.Context, token, method string, body map[string]any) error {
	exists, err := c.hasAuth(ctx, token, method)
	if err != nil || exists {
		return err
	}
	return c.request(ctx, http.MethodPost, "sys/auth/"+strings.Trim(method, "/"), token, nil, body, nil)
}

func (c *vaultClient) transitKeyExists(ctx context.Context, token, keyName string) (bool, error) {
	status, err := c.requestStatus(ctx, http.MethodGet, "transit/keys/"+keyName, token, nil, nil, nil)
	if err != nil {
		return false, err
	}
	if status == http.StatusOK {
		return true, nil
	}
	if status == http.StatusNotFound || status == http.StatusBadRequest {
		return false, nil
	}
	return false, fmt.Errorf("vault GET transit/keys/%s returned HTTP %d", keyName, status)
}

func (c *vaultClient) ensureTransitEd25519Key(ctx context.Context, token, keyName string) error {
	exists, err := c.transitKeyExists(ctx, token, keyName)
	if err != nil {
		return fmt.Errorf("inspect Transit key %s: %w", keyName, err)
	}
	if !exists {
		if err := c.request(ctx, http.MethodPost, "transit/keys/"+keyName, token, nil, map[string]any{
			"type":                   "ed25519",
			"exportable":             false,
			"allow_plaintext_backup": false,
			"auto_rotate_period":     0,
		}, nil); err != nil {
			return fmt.Errorf("create Transit key %s: %w", keyName, err)
		}
	}
	if err := c.request(ctx, http.MethodPost, "transit/keys/"+keyName+"/config", token, nil, map[string]any{
		"deletion_allowed": false,
	}, nil); err != nil {
		return fmt.Errorf("lock Transit key %s deletion: %w", keyName, err)
	}
	return nil
}

func (c *vaultClient) requestStatus(ctx context.Context, method, path, token string, headers map[string]string, body any, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/v1/"+strings.TrimLeft(path, "/"), reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("vault %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || resp.StatusCode == http.StatusNoContent {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			return resp.StatusCode, nil
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode Vault %s %s response: %w", method, path, err)
		}
		return resp.StatusCode, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, nil
}

func (c *vaultClient) request(ctx context.Context, method, path, token string, headers map[string]string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/v1/"+strings.TrimLeft(path, "/"), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("vault %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("vault %s %s returned HTTP %d", method, path, resp.StatusCode)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode Vault %s %s response: %w", method, path, err)
	}
	return nil
}

func readVaultInitMaterial(stateDir string) (vaultInitMaterial, error) {
	raw, err := os.ReadFile(filepath.Join(stateDir, secretDirName, "vault_init.json"))
	if err != nil {
		return vaultInitMaterial{}, fmt.Errorf("read Vault operator material: %w", err)
	}
	var material vaultInitMaterial
	if err := json.Unmarshal(raw, &material); err != nil {
		return vaultInitMaterial{}, fmt.Errorf("decode Vault operator material: %w", err)
	}
	if material.UnsealKey == "" || material.RootToken == "" {
		return vaultInitMaterial{}, errors.New("vault operator material is incomplete")
	}
	return material, nil
}

func unsealVault(client *vaultClient, unsealKey string) error {
	var response struct {
		Sealed bool `json:"sealed"`
	}
	if err := client.request(context.Background(), http.MethodPost, "sys/unseal", "", nil, map[string]string{"key": unsealKey}, &response); err != nil {
		return err
	}
	if response.Sealed {
		return errors.New("vault remains sealed after manual unseal")
	}
	return nil
}

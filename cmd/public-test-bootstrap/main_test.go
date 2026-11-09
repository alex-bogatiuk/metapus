package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"metapus/internal/core/id"
	walletcatalog "metapus/internal/domain/catalogs/wallet"
	domaincrypto "metapus/internal/domain/crypto"
)

func TestInitializeAndFinalizeStateKeepsSecretsOutOfEnv(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	state, err := initializeState(stateDir, "176.215.182.82", "test", now)
	if err != nil {
		t.Fatal(err)
	}
	chainConfig, err := os.ReadFile(filepath.Join(state.StateDir, "crypto", "chains.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chainConfig), `"TRON-NILE"`) ||
		!strings.Contains(string(chainConfig), `"apiKeySecretFile": "/run/secrets/tron_api_key"`) {
		t.Fatal("chain config does not contain the Nile topology and secret-file reference")
	}
	if strings.Contains(string(chainConfig), "TRON_API_KEY") {
		t.Fatal("chain config contains a legacy secret environment variable")
	}

	envRaw, err := os.ReadFile(state.EnvFile)
	if err != nil {
		t.Fatal(err)
	}
	envText := string(envRaw)
	if !strings.Contains(envText, "PUBLIC_TEST_SECRET_DIR=") {
		t.Fatal("public env does not point at the secret directory")
	}

	secretNames := []string{
		"postgres_password",
		"jwt_secret",
		"auth_mfa_encryption_key",
		"automation_encryption_key",
		"nile_mnemonic",
		"crypto_policy_signing_private_key",
	}
	unique := make(map[string]string, len(secretNames))
	for _, name := range secretNames {
		value, readErr := readSecret(filepath.Join(state.StateDir, secretDirName, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if value == "" {
			t.Fatalf("%s is empty", name)
		}
		if strings.Contains(envText, value) {
			t.Fatalf("%s leaked into the public env file", name)
		}
		if previous, exists := unique[value]; exists {
			t.Fatalf("%s and %s unexpectedly share a secret value", previous, name)
		}
		unique[value] = name
	}
	mnemonic, _ := readSecret(filepath.Join(state.StateDir, secretDirName, "nile_mnemonic"))
	if words := len(strings.Fields(mnemonic)); words != 24 {
		t.Fatalf("expected a 24-word mnemonic, got %d words", words)
	}

	tenantID := id.New().String()
	if err := finalizeTenantState(state.StateDir, tenantID, "mt_public_test", now); err != nil {
		t.Fatal(err)
	}
	policyJWS, err := readSecret(filepath.Join(state.StateDir, secretDirName, "crypto_approval_policy_bundle_jws"))
	if err != nil {
		t.Fatal(err)
	}
	policyPublicRaw := envValue(t, state.EnvFile, "CRYPTO_APPROVAL_POLICY_PUBLIC_KEY")
	policyPublic, err := domaincrypto.ParseEd25519PublicKey(policyPublicRaw)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := domaincrypto.NewSignedApprovalPolicy(policyJWS, policyPublic, now)
	if err != nil {
		t.Fatal(err)
	}
	if signed.Policy.TenantID != tenantID {
		t.Fatalf("policy tenant = %s, want %s", signed.Policy.TenantID, tenantID)
	}

	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(filepath.Join(state.StateDir, secretDirName, "jwt_secret"))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("secret mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestInitializeSepoliaStateUsesTwoIndependentRPCProviders(t *testing.T) {
	state, err := initializeStateForNetwork(filepath.Join(t.TempDir(), "state"), "127.0.0.1", "test", publicTestNetworkEVMSepolia, "Admin123!", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(state.StateDir, "crypto", "chains.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, expected := range []string{`"ETHEREUM-SEPOLIA"`, `"publicnode-primary"`, `"zeroxrpc-secondary"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Sepolia chain config is missing %s", expected)
		}
	}
	if envValue(t, state.EnvFile, "CRYPTO_NETWORK_CODE") != "ETHEREUM-SEPOLIA" {
		t.Fatal("Sepolia state did not select the EVM preflight network")
	}
}

func TestInitializeBothNetworksWritesNileAndSepoliaTopology(t *testing.T) {
	state, err := initializeStateForNetwork(filepath.Join(t.TempDir(), "state"), "127.0.0.1", "test", publicTestNetworkBoth, "Admin123!", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(state.StateDir, "crypto", "chains.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, expected := range []string{`"TRON-NILE"`, `"ETHEREUM-SEPOLIA"`, `"trongrid-primary"`, `"disableKeepAlives"`, `"publicnode-primary"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("dual-network chain config is missing %s", expected)
		}
	}
	if envValue(t, state.EnvFile, "CRYPTO_NETWORK_CODE") != "TRON-NILE" {
		t.Fatal("dual-network state should default preflight to TRON-NILE")
	}
}

func TestConfigureChainsPreservesExistingNileEndpoints(t *testing.T) {
	state, err := initializeStateForNetwork(filepath.Join(t.TempDir(), "state"), "127.0.0.1", "test", publicTestNetworkTronNile, "Admin123!", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state.StateDir, "crypto", "chains.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	custom := strings.ReplaceAll(string(raw), "https://nile.trongrid.io", "https://nile.trongrid.io:19443")
	custom = strings.Replace(custom, `"networks": {`, `"note": "keep", "networks": {`, 1)
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigureChains([]string{"--state-dir", state.StateDir}); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "https://nile.trongrid.io:19443") ||
		!strings.Contains(string(updated), `"ETHEREUM-SEPOLIA"`) ||
		!strings.Contains(string(updated), `"note": "keep"`) {
		t.Fatal("configure-chains must keep existing configuration and add Sepolia")
	}
}

func TestWorkerVaultPolicyAllowsOnlyTenantScopedWalletDerivation(t *testing.T) {
	tenantID := "00000000-0000-0000-0000-000000000001"
	policy := workerVaultPolicy(tenantID)
	webhookDataExpected := `path "secret/data/merchant-webhooks/*" {
  capabilities = ["create", "read", "update"]
}`
	if !strings.Contains(policy, webhookDataExpected) {
		t.Fatal("worker policy does not allow webhook-secret KV data access")
	}
	webhookMetadataExpected := `path "secret/metadata/merchant-webhooks/*" {
  capabilities = ["delete"]
}`
	if !strings.Contains(policy, webhookMetadataExpected) {
		t.Fatal("worker policy does not allow webhook-secret metadata deletion")
	}
	expected := `path "tron/tenants/` + tenantID + `/wallets/derive" {
  capabilities = ["update"]
}`
	if !strings.Contains(policy, expected) {
		t.Fatal("worker policy does not allow tenant-scoped wallet derivation")
	}
	evmExpected := `path "evm/tenants/` + tenantID + `/wallets/derive" {
  capabilities = ["update"]
}`
	if !strings.Contains(policy, evmExpected) {
		t.Fatal("worker policy does not allow tenant-scoped EVM wallet derivation")
	}
	if strings.Contains(policy, `tron/tenants/*`) || strings.Contains(policy, `evm/tenants/*`) || strings.Contains(policy, `wallets/*`) {
		t.Fatal("worker policy contains a wildcard wallet capability")
	}
}

func TestVaultBootstrapConfiguresWebhookSecretKVv2(t *testing.T) {
	raw, err := os.ReadFile("vault.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `ensureMount(ctx, rootToken, "secret", map[string]any{`) ||
		!strings.Contains(string(raw), `"options": map[string]string{"version": "2"}`) {
		t.Fatal("Vault bootstrap must enable the webhook-secret KV v2 mount")
	}
}

func TestInitialNileWalletSpecsSeparateLiquidityAndNetworkFees(t *testing.T) {
	specs := initialNileWalletSpecs(321)
	if len(specs) != 4 {
		t.Fatalf("wallet count = %d, want 4", len(specs))
	}

	liquidity, err := derivedWalletByRole(specs, walletcatalog.WalletRoleLiquidityHot)
	if err != nil {
		t.Fatal(err)
	}
	fee, err := derivedWalletByRole(specs, walletcatalog.WalletRoleNetworkFee)
	if err != nil {
		t.Fatal(err)
	}
	if liquidity.Code != "HOT-TRON-001" || liquidity.DerivationPath != "m/44'/195'/0'/0/0" {
		t.Fatalf("unexpected liquidity wallet: %+v", liquidity)
	}
	if fee.Code != "FEE-TRON-001" || fee.DerivationPath != "m/44'/195'/0'/0/1" {
		t.Fatalf("unexpected fee wallet: %+v", fee)
	}
	if liquidity.DerivationPath == fee.DerivationPath {
		t.Fatal("liquidity and network fee wallets share a derivation path")
	}

	pools := derivedWalletsByRole(specs, walletcatalog.WalletRoleMerchantPool)
	if len(pools) != 2 || pools[0].DerivationPath != "m/44'/195'/321'/0/1" || pools[1].DerivationPath != "m/44'/195'/321'/0/2" {
		t.Fatalf("unexpected pool wallets: %+v", pools)
	}
}

func TestInitialSepoliaWalletSpecsUseEVMNamespace(t *testing.T) {
	specs := initialSepoliaWalletSpecs(321)
	if len(specs) != 4 {
		t.Fatalf("wallet count = %d, want 4", len(specs))
	}
	liquidity, err := derivedWalletByRole(specs, walletcatalog.WalletRoleLiquidityHot)
	if err != nil {
		t.Fatal(err)
	}
	fee, err := derivedWalletByRole(specs, walletcatalog.WalletRoleNetworkFee)
	if err != nil {
		t.Fatal(err)
	}
	if liquidity.DerivationPath != "m/44'/60'/0'/0/0" || fee.DerivationPath != "m/44'/60'/0'/0/1" {
		t.Fatalf("unexpected EVM custody paths: liquidity=%s fee=%s", liquidity.DerivationPath, fee.DerivationPath)
	}
	for _, wallet := range specs {
		if !strings.HasPrefix(wallet.DerivationPath, "m/44'/60'/") {
			t.Fatalf("non-EVM derivation path: %s", wallet.DerivationPath)
		}
	}
}

func TestSepoliaWalletPoolDoesNotConfigureOnChainActivation(t *testing.T) {
	sepolia := walletPoolActivationParamsFor("ethereum_sepolia")
	for i, value := range sepolia {
		if value != nil {
			t.Fatalf("Sepolia activation parameter %d = %v, want nil", i, value)
		}
	}
	tron := walletPoolActivationParamsFor("tron_nile")
	for i, value := range tron {
		if value == nil {
			t.Fatalf("TRON activation parameter %d is nil", i)
		}
	}
	if got := poolActivationStatusFor("ETHEREUM-SEPOLIA"); got != walletcatalog.ChainActivationActive {
		t.Fatalf("Sepolia pool activation status = %s, want active", got)
	}
	if got := poolActivationStatusFor("TRON-NILE"); got != walletcatalog.ChainActivationUnknown {
		t.Fatalf("TRON pool activation status = %s, want unknown", got)
	}
}

func TestValidateDerivedWalletSetRequiresExplicitUniqueRolesAndIdentities(t *testing.T) {
	valid := derivedWalletSet{Wallets: initialNileWalletSpecs(321)}
	for i := range valid.Wallets {
		valid.Wallets[i].Address = "TAddress" + valid.Wallets[i].Code
		valid.Wallets[i].VaultKeyID = "vault:" + valid.Wallets[i].Code
	}
	if err := validateDerivedWalletSet(valid); err != nil {
		t.Fatalf("valid wallet set rejected: %v", err)
	}

	duplicateRole := valid
	duplicateRole.Wallets = append([]derivedWallet(nil), valid.Wallets...)
	duplicateRole.Wallets[1].Role = walletcatalog.WalletRoleLiquidityHot
	if err := validateDerivedWalletSet(duplicateRole); err == nil || !strings.Contains(err.Error(), "want 1/1/2") {
		t.Fatalf("duplicate role error = %v", err)
	}

	duplicatePath := valid
	duplicatePath.Wallets = append([]derivedWallet(nil), valid.Wallets...)
	duplicatePath.Wallets[1].DerivationPath = duplicatePath.Wallets[0].DerivationPath
	if err := validateDerivedWalletSet(duplicatePath); err == nil || !strings.Contains(err.Error(), "duplicate derivation path") {
		t.Fatalf("duplicate path error = %v", err)
	}
}

func envValue(t *testing.T, path, name string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if after, ok := strings.CutPrefix(line, name+"="); ok {
			return after
		}
	}
	t.Fatalf("%s is missing", name)
	return ""
}

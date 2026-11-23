// Command public-test-bootstrap creates and finalizes the local secret state
// used by the disposable public Nile and Sepolia deployment. It never prints secret values.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	bip39 "github.com/fbsobreira/go-bip39"

	domaincrypto "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
)

const (
	secretDirName               = "secrets"
	envFileName                 = "public-test.env"
	publicTestNetworkTronNile   = "tron-nile"
	publicTestNetworkEVMSepolia = "ethereum-sepolia"
	publicTestNetworkBoth       = "both"
)

type generatedKeyPair struct {
	Public  string
	Private string
}

type initializedState struct {
	StateDir    string
	EnvFile     string
	PoolAccount int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: public-test-bootstrap <init|finalize|vault-init|vault-unseal|vault-bootstrap|vault-worker-policy|vault-wrap-secret-id|configure-db|configure-chains|vault-config|sweep-policy> [options]")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "finalize":
		return runFinalize(args[1:])
	case "vault-init":
		return runVaultInit(args[1:])
	case "vault-unseal":
		return runVaultUnseal(args[1:])
	case "vault-bootstrap":
		return runVaultBootstrap(args[1:])
	case "vault-worker-policy":
		return runVaultWorkerPolicy(args[1:])
	case "vault-wrap-secret-id":
		return runVaultWrapSecretID(args[1:])
	case "configure-db":
		return runConfigureDB(args[1:])
	case "configure-chains":
		return runConfigureChains(args[1:])
	case "vault-config":
		return runVaultConfig(args[1:])
	case "sweep-policy":
		return runSweepPolicy(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "absolute directory outside the repository")
	publicIP := fs.String("public-ip", "176.215.182.82", "public IPv4 address")
	version := fs.String("version", "dev", "Metapus version label")
	network := fs.String("network", publicTestNetworkBoth, "test network: tron-nile, ethereum-sepolia, or both")
	adminPassword := fs.String("admin-password", "Admin123!", "initial admin user password")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required")
	}
	ip := net.ParseIP(*publicIP)
	if ip == nil || ip.To4() == nil || strings.Contains(*publicIP, ":") {
		return errors.New("--public-ip must be a literal IPv4 address")
	}

	state, err := initializeStateForNetwork(*stateDir, *publicIP, *version, *network, *adminPassword, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Printf("Public test state initialized outside the repository.\nState directory: %s\nEnv file: %s\nPool account: %d\nAdmin email: admin@metapus.io\nAdmin password file: %s\n", state.StateDir, state.EnvFile, state.PoolAccount, filepath.Join(state.StateDir, secretDirName, "admin_password"))
	return nil
}

func initializeState(stateDir, publicIP, version string, now time.Time) (initializedState, error) {
	return initializeStateForNetwork(stateDir, publicIP, version, publicTestNetworkTronNile, "Admin123!", now)
}

func initializeStateForNetwork(stateDir, publicIP, version, network, adminPassword string, now time.Time) (initializedState, error) {
	network = strings.ToLower(strings.TrimSpace(network))
	if network != publicTestNetworkTronNile && network != publicTestNetworkEVMSepolia && network != publicTestNetworkBoth {
		return initializedState{}, fmt.Errorf("unsupported public test network %q", network)
	}
	absState, err := filepath.Abs(stateDir)
	if err != nil {
		return initializedState{}, fmt.Errorf("resolve state directory: %w", err)
	}
	if err := ensureOutsideWorkingTree(absState); err != nil {
		return initializedState{}, err
	}
	if entries, readErr := os.ReadDir(absState); readErr == nil && len(entries) != 0 {
		return initializedState{}, fmt.Errorf("state directory must be new or empty: %s", absState)
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return initializedState{}, fmt.Errorf("inspect state directory: %w", readErr)
	}
	if err := os.MkdirAll(absState, 0o700); err != nil {
		return initializedState{}, fmt.Errorf("create state directory: %w", err)
	}
	secretDir := filepath.Join(absState, secretDirName)
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return initializedState{}, fmt.Errorf("create secret directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absState, "vault", "bootstrap"), 0o700); err != nil {
		return initializedState{}, fmt.Errorf("create Vault bootstrap directory: %w", err)
	}
	cryptoDir := filepath.Join(absState, "crypto")
	if err := os.MkdirAll(cryptoDir, 0o700); err != nil {
		return initializedState{}, fmt.Errorf("create crypto config directory: %w", err)
	}
	if err := restrictStatePermissions(absState); err != nil {
		return initializedState{}, err
	}

	secretValues := make(map[string]string)
	secretValues["postgres_password"], err = randomBase64URL(32)
	if err != nil {
		return initializedState{}, err
	}
	secretValues["jwt_secret"], err = randomBase64URL(32)
	if err != nil {
		return initializedState{}, err
	}
	secretValues["auth_mfa_encryption_key"], err = randomBase64(32)
	if err != nil {
		return initializedState{}, err
	}
	secretValues["automation_encryption_key"], err = randomBase64URL(24)
	if err != nil {
		return initializedState{}, err
	}
	if strings.TrimSpace(adminPassword) != "" {
		secretValues["admin_password"] = strings.TrimSpace(adminPassword)
	} else {
		secretValues["admin_password"], err = randomBase64URL(24)
		if err != nil {
			return initializedState{}, err
		}
	}
	secretValues["meta_database_url"] = postgresURL(secretValues["postgres_password"], "tenants")
	secretValues["tenant_database_url"] = ""
	secretValues["crypto_approval_policy_bundle_jws"] = ""
	secretValues["tron_api_key"] = ""

	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return initializedState{}, fmt.Errorf("generate mnemonic entropy: %w", err)
	}
	secretValues["nile_mnemonic"], err = bip39.NewMnemonic(entropy)
	if err != nil {
		return initializedState{}, fmt.Errorf("generate mnemonic: %w", err)
	}

	keyNames := []string{"policy"}
	keys := make(map[string]generatedKeyPair, len(keyNames))
	for _, name := range keyNames {
		pair, keyErr := newEd25519KeyPair()
		if keyErr != nil {
			return initializedState{}, fmt.Errorf("generate %s key: %w", name, keyErr)
		}
		keys[name] = pair
		secretValues["crypto_"+name+"_signing_private_key"] = pair.Private
	}

	for name, value := range secretValues {
		if err := writeExclusive(filepath.Join(secretDir, name), []byte(value), 0o600); err != nil {
			return initializedState{}, err
		}
	}
	if err := generateVaultTLS(filepath.Join(absState, "vault", "tls"), now); err != nil {
		return initializedState{}, err
	}
	chainConfig := map[string]any{"networks": chainTopologyFor(network)}
	chainConfigRaw, err := json.MarshalIndent(chainConfig, "", "  ")
	if err != nil {
		return initializedState{}, fmt.Errorf("marshal chain config: %w", err)
	}
	if err := writeExclusive(filepath.Join(cryptoDir, "chains.json"), append(chainConfigRaw, '\n'), 0o600); err != nil {
		return initializedState{}, err
	}

	poolAccount, err := randomPoolAccount()
	if err != nil {
		return initializedState{}, err
	}
	envValues := []string{
		"PUBLIC_IP=" + publicIP,
		"NGINX_MODE=acme",
		"PUBLIC_TEST_STATE_DIR=" + envPath(absState),
		"PUBLIC_TEST_SECRET_DIR=" + envPath(secretDir),
		"METAPUS_VERSION=" + version,
		// Compose validates required interpolation even for inactive profiles.
		// Finalize replaces this non-routable bootstrap sentinel with the real
		// tenant ID before any application service is started.
		"TENANT_ID=00000000-0000-0000-0000-000000000001",
		"TENANT_DB=",
		fmt.Sprintf("POOL_ACCOUNT=%d", poolAccount),
		"CRYPTO_PROCESSING_ENABLED=true",
		"CRYPTO_CHAIN_CONFIG_FILE=/run/metapus/crypto/chains.json",
		"CRYPTO_NETWORK_CODE=" + map[string]string{
			publicTestNetworkTronNile:   "TRON-NILE",
			publicTestNetworkEVMSepolia: "ETHEREUM-SEPOLIA",
			publicTestNetworkBoth:       "TRON-NILE",
		}[network],
		"ADMIN_SECURITY_MODE=warn",
		"ADMIN_ALLOWED_CIDRS=",
		"CRYPTO_APPROVAL_POLICY_PUBLIC_KEY=" + keys["policy"].Public,
		"CRYPTO_INTENT_SIGNING_PUBLIC_KEY=",
		"CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY=",
		"CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY=",
		"CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY=",
		"CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY=",
		"VAULT_TEST_PORT=8200",
		"VAULT_CA_CERT_FILE=" + envPath(filepath.Join(absState, "vault", "tls", "ca.crt")),
		"VAULT_ROLE_ID_FILE=" + envPath(filepath.Join(absState, "vault", "bootstrap", "role-id")),
		"VAULT_BOOTSTRAP_DIR=" + envPath(filepath.Join(absState, "vault", "bootstrap")),
	}
	envFile := filepath.Join(absState, envFileName)
	if err := writeExclusive(envFile, []byte(strings.Join(envValues, "\n")+"\n"), 0o600); err != nil {
		return initializedState{}, err
	}
	return initializedState{
		StateDir:    absState,
		EnvFile:     envFile,
		PoolAccount: poolAccount,
	}, nil
}

func runFinalize(args []string) error {
	fs := flag.NewFlagSet("finalize", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	tenantID := fs.String("tenant-id", "", "new tenant UUID")
	tenantDB := fs.String("tenant-db", "", "new tenant database name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" || *tenantID == "" || *tenantDB == "" {
		return errors.New("--state-dir, --tenant-id, and --tenant-db are required")
	}
	if _, err := id.Parse(*tenantID); err != nil {
		return fmt.Errorf("invalid tenant UUID: %w", err)
	}
	if !validDatabaseName(*tenantDB) {
		return errors.New("--tenant-db contains unsupported characters")
	}
	if err := finalizeTenantState(*stateDir, *tenantID, *tenantDB, time.Now().UTC()); err != nil {
		return err
	}
	fmt.Printf("Tenant-specific policy and database configuration finalized for tenant %s.\n", *tenantID)
	return nil
}

func finalizeTenantState(stateDir, tenantID, tenantDB string, now time.Time) error {
	absState, err := filepath.Abs(stateDir)
	if err != nil {
		return err
	}
	secretDir := filepath.Join(absState, secretDirName)
	policyPrivateRaw, err := readSecret(filepath.Join(secretDir, "crypto_policy_signing_private_key"))
	if err != nil {
		return err
	}
	policyPrivate, err := domaincrypto.ParseEd25519PrivateKey(policyPrivateRaw)
	if err != nil {
		return fmt.Errorf("parse policy private key: %w", err)
	}
	policyJWS, err := domaincrypto.SignEdDSAJWS(domaincrypto.DefaultMoneyMovementApprovalPolicy(tenantID, now), policyPrivate)
	if err != nil {
		return fmt.Errorf("sign approval policy: %w", err)
	}
	if err := writeAtomic(filepath.Join(secretDir, "crypto_approval_policy_bundle_jws"), []byte(policyJWS), 0o600); err != nil {
		return err
	}
	postgresPassword, err := readSecret(filepath.Join(secretDir, "postgres_password"))
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(secretDir, "tenant_database_url"), []byte(postgresURL(postgresPassword, tenantDB)), 0o600); err != nil {
		return err
	}
	return updateEnvFile(filepath.Join(absState, envFileName), map[string]string{
		"TENANT_ID": tenantID,
		"TENANT_DB": tenantDB,
	})
}

func runVaultConfig(args []string) error {
	fs := flag.NewFlagSet("vault-config", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	tenantID := fs.String("tenant-id", "", "tenant UUID")
	family := fs.String("family", "tron", "custody family: tron or evm")
	kid := fs.String("authorization-kid", "", "active Transit KID")
	publicKeyFile := fs.String("authorization-public-key-file", "", "file containing the Transit public key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" || *tenantID == "" || *kid == "" || *publicKeyFile == "" {
		return errors.New("--state-dir, --tenant-id, --authorization-kid, and --authorization-public-key-file are required")
	}
	if _, err := id.Parse(*tenantID); err != nil {
		return fmt.Errorf("invalid tenant UUID: %w", err)
	}
	publicKey, err := readSecret(*publicKeyFile)
	if err != nil {
		return err
	}
	approvalPublicKeys, err := approvalPublicKeysFromEnvFile(filepath.Join(*stateDir, envFileName))
	if err != nil {
		return err
	}
	if err := createVaultTenantConfigForFamily(*stateDir, *tenantID, *kid, publicKey, approvalPublicKeys, *family); err != nil {
		return err
	}
	fmt.Printf("Vault tenant signing configuration created for tenant %s.\n", *tenantID)
	return nil
}

func createVaultTenantConfigForFamily(stateDir, tenantID, kid, authorizationPublicKey string, approvalPublicKeys map[string]string, family string) error {
	family = strings.ToLower(strings.TrimSpace(family))
	if family != "tron" && family != "evm" {
		return fmt.Errorf("unsupported Vault custody family %q", family)
	}
	absState, err := filepath.Abs(stateDir)
	if err != nil {
		return err
	}
	secretDir := filepath.Join(absState, secretDirName)
	mnemonic, err := readSecret(filepath.Join(secretDir, "nile_mnemonic"))
	if err != nil {
		return err
	}
	if !bip39.IsMnemonicValid(mnemonic) {
		return errors.New("stored test wallet mnemonic is invalid")
	}
	passphrase := ""
	if family == "evm" {
		// The disposable test stand may intentionally reuse the operator's
		// mnemonic, but the EVM mount still receives family-separated seed
		// material instead of the byte-identical TRON seed.
		passphrase = "metapus-evm-sepolia-test-v1"
	}
	seed := base64.RawURLEncoding.EncodeToString(bip39.NewSeed(mnemonic, passphrase))
	policyPrivateRaw, err := readSecret(filepath.Join(secretDir, "crypto_policy_signing_private_key"))
	if err != nil {
		return err
	}
	policyPrivate, err := domaincrypto.ParseEd25519PrivateKey(policyPrivateRaw)
	if err != nil {
		return fmt.Errorf("parse policy private key: %w", err)
	}
	policyPublic := base64.RawURLEncoding.EncodeToString(policyPrivate.Public().(ed25519.PublicKey))
	schemaVersion := "tron_tenant_signing_config.v3"
	config := map[string]any{
		"schema_version": schemaVersion,
		"authorization_keyring": map[string]string{
			kid: strings.TrimSpace(authorizationPublicKey),
		},
		"policy_public_key":          policyPublic,
		"intent_public_key":          approvalPublicKeys["intent"],
		"risk_assessment_public_key": approvalPublicKeys["risk_assessment"],
		"policy_decision_public_key": approvalPublicKeys["policy_decision"],
		"admin_approval_public_key":  approvalPublicKeys["admin_approval"],
		"risk_approval_public_key":   approvalPublicKeys["risk_approval"],
		"hd_seed":                    seed,
	}
	if family == "evm" {
		config["schema_version"] = "evm_tenant_signing_config.v1"
		config["allowed_chain_ids"] = []string{"11155111"}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(secretDir, "vault_tenant_signing_config.json"), raw, 0o600)
}

func approvalPublicKeysFromEnvFile(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	wanted := map[string]string{
		"CRYPTO_INTENT_SIGNING_PUBLIC_KEY":          "intent",
		"CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY": "risk_assessment",
		"CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY": "policy_decision",
		"CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY":  "admin_approval",
		"CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY":   "risk_approval",
	}
	keys := make(map[string]string, len(wanted))
	for line := range strings.SplitSeq(string(raw), "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		if role, exists := wanted[name]; exists && strings.TrimSpace(value) != "" {
			keys[role] = strings.TrimSpace(value)
		}
	}
	if len(keys) != len(wanted) {
		return nil, errors.New("approval Transit public keys are not initialized")
	}
	return keys, nil
}

func runSweepPolicy(args []string) error {
	fs := flag.NewFlagSet("sweep-policy", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	hotAddress := fs.String("hot-address", "", "derived tenant hot wallet")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" || strings.TrimSpace(*hotAddress) == "" {
		return errors.New("--state-dir and --hot-address are required")
	}
	policy := map[string]any{
		"schema_version": "tron_sweep_destinations.v1",
		"version":        time.Now().UTC().Format("2006-01-02.150405"),
		"destinations": map[string][]string{
			"tron_nile": {strings.TrimSpace(*hotAddress)},
		},
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	absState, err := filepath.Abs(*stateDir)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(absState, secretDirName, "vault_sweep_destinations.json"), raw, 0o600); err != nil {
		return err
	}
	fmt.Println("Tenant sweep policy created with exactly one derived Nile destination.")
	return nil
}

func nileChainTopology() map[string]any {
	primary := map[string]any{
		"providerId":        "trongrid-primary",
		"priority":          0,
		"url":               "https://nile.trongrid.io",
		"disableKeepAlives": true,
		"apiKeySecretFile":  "/run/secrets/tron_api_key",
	}
	secondary := map[string]any{"providerId": "nileex-secondary", "priority": 10, "url": "https://api.nileex.io"}
	return map[string]any{
		"TRON-NILE": map[string]any{
			"roles": map[string]any{
				"indexer":  []map[string]any{primary},
				"fullnode": []map[string]any{primary, secondary},
				"solidity": []map[string]any{primary, secondary},
			},
		},
	}
}

func sepoliaChainTopology() map[string]any {
	return map[string]any{
		"ETHEREUM-SEPOLIA": map[string]any{"roles": map[string]any{
			"rpc": []map[string]any{
				{"providerId": "publicnode-primary", "priority": 0, "url": "https://ethereum-sepolia-rpc.publicnode.com"},
				{"providerId": "zeroxrpc-secondary", "priority": 10, "url": "https://0xrpc.io/sep"},
			},
		}},
	}
}

func chainTopologyFor(network string) map[string]any {
	switch network {
	case publicTestNetworkEVMSepolia:
		return sepoliaChainTopology()
	case publicTestNetworkBoth:
		networks := nileChainTopology()
		for code, topology := range sepoliaChainTopology() {
			networks[code] = topology
		}
		return networks
	default:
		return nileChainTopology()
	}
}

func runConfigureChains(args []string) error {
	fs := flag.NewFlagSet("configure-chains", flag.ContinueOnError)
	stateDir := fs.String("state-dir", "", "initialized public test state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required")
	}
	path := filepath.Join(*stateDir, "crypto", "chains.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read chain configuration: %w", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return fmt.Errorf("decode chain configuration: %w", err)
	}
	networks, ok := config["networks"].(map[string]any)
	if !ok || len(networks) == 0 {
		return errors.New("chain configuration has no networks")
	}
	for code, topology := range chainTopologyFor(publicTestNetworkBoth) {
		if _, exists := networks[code]; !exists {
			networks[code] = topology
		}
	}
	updated, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode chain configuration: %w", err)
	}
	if err := writeAtomic(path, append(updated, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Println("Nile and Sepolia RPC configuration is present; existing endpoints were preserved.")
	return nil
}

func newEd25519KeyPair() (generatedKeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return generatedKeyPair{}, err
	}
	return generatedKeyPair{
		Public:  base64.RawURLEncoding.EncodeToString(publicKey),
		Private: base64.RawURLEncoding.EncodeToString(privateKey),
	}, nil
}

func randomBase64URL(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func randomBase64(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func randomPoolAccount() (int, error) {
	limit := new(big.Int).SetInt64(2_000_000_000)
	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return 0, fmt.Errorf("generate pool account: %w", err)
	}
	return int(value.Int64() + 10_000), nil
}

func postgresURL(password, database string) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword("metapus", password),
		Host:   "postgres:5432",
		Path:   "/" + database,
	}
	u.RawQuery = "sslmode=disable"
	return u.String()
}

func validDatabaseName(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func ensureOutsideWorkingTree(target string) error {
	workingDir, err := os.Getwd()
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(workingDir, target)
	if err != nil {
		return err
	}
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return fmt.Errorf("state directory must be outside the repository working tree: %s", target)
	}
	return nil
}

func envPath(path string) string {
	return filepath.ToSlash(path)
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		// Docker Desktop file sharing can reject chmod from a Linux container
		// even when the file was created with the requested mode. Accept only
		// that already-safe state; any looser permission remains an error.
		info, statErr := os.Stat(path)
		if statErr == nil && info.Mode().Perm() == mode.Perm() {
			return nil
		}
		return err
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".metapus-secret-*")
	if err != nil {
		return fmt.Errorf("create temporary secret: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		_ = os.RemoveAll(path)
	} else if runtime.GOOS == "windows" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		// Docker Desktop file sharing can reject chmod from a Linux container
		// even when the file was created with the requested mode. Accept only
		// that already-safe state; any looser permission remains an error.
		info, statErr := os.Stat(path)
		if statErr == nil && info.Mode().Perm() == mode.Perm() {
			return nil
		}
		return err
	}
	return nil
}

func readSecret(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) > 1<<20 {
		return "", fmt.Errorf("secret file is too large: %s", path)
	}
	return strings.TrimRight(string(raw), "\r\n"), nil
}

func updateEnvFile(path string, updates map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	seen := make(map[string]bool, len(updates))
	for i, line := range lines {
		for name, value := range updates {
			if strings.HasPrefix(line, name+"=") {
				lines[i] = name + "=" + value
				seen[name] = true
			}
		}
	}
	names := make([]string, 0, len(updates))
	for name := range updates {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !seen[name] {
			lines = append(lines, name+"="+updates[name])
		}
	}
	return writeAtomic(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func generateVaultTLS(outputDir string, now time.Time) error {
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return err
	}
	caKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return fmt.Errorf("generate Vault CA key: %w", err)
	}
	caTemplate := x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "Metapus Vault Test CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(30 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create Vault CA certificate: %w", err)
	}

	serverKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return fmt.Errorf("generate Vault server key: %w", err)
	}
	serverTemplate := x509.Certificate{
		SerialNumber: randomSerial(),
		Subject:      pkix.Name{CommonName: "vault-test"},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(30 * 24 * time.Hour),
		DNSNames:     []string{"vault-test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create Vault server certificate: %w", err)
	}
	serverKeyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return err
	}
	if err := writeExclusive(filepath.Join(outputDir, "ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o444); err != nil {
		return err
	}
	if err := writeExclusive(filepath.Join(outputDir, "server.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o444); err != nil {
		return err
	}
	return writeExclusive(filepath.Join(outputDir, "server.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER}), 0o400)
}

func randomSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		panic(err)
	}
	return serial
}

func restrictStatePermissions(path string) error {
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve current Windows user: %w", err)
	}
	// Windows has ACLs rather than POSIX mode bits. Limit inheritance and grant
	// only the current user and SYSTEM access to this disposable state tree.
	args := []string{
		path,
		"/inheritance:r",
		"/grant:r", current.Username + ":(OI)(CI)F",
		"/grant:r", "SYSTEM:(OI)(CI)F",
	}
	if err := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe"), args...).Run(); err != nil {
		return fmt.Errorf("restrict Windows ACL for %s", path)
	}
	return nil
}

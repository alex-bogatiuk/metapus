// Command aml-testnet-config configures a credential-free testnet AML emulator.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/platform/secretfile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	namespaceFlag := flag.String("tenant-id", "", "Enrolled Vault namespace UUID")
	accountFlag := flag.String("account-id", "", "Stable emulator account UUID")
	merchantFlag := flag.String("merchant-id", "", "Merchant UUID")
	networkCode := flag.String("network-code", "", "TRON Nile or Ethereum Sepolia catalog code")
	seed := flag.String("seed", "", "Reproducible public seed; omitted creates one once")
	delay := flag.Int("max-delay-seconds", 10, "Maximum simulated polling delay (0..60)")
	unknown := flag.Int("unknown-rate-bp", 1000, "Probability of unscored evidence, basis points (0..10000)")
	actor := flag.String("actor", "", "Operator identity")
	flag.Parse()
	namespace, err := id.Parse(*namespaceFlag)
	if err != nil || id.IsNil(namespace) {
		return fmt.Errorf("tenant UUID required")
	}
	account, err := id.Parse(*accountFlag)
	if err != nil || id.IsNil(account) {
		return fmt.Errorf("stable account UUID required")
	}
	merchant, err := id.Parse(*merchantFlag)
	if err != nil || id.IsNil(merchant) {
		return fmt.Errorf("merchant UUID required")
	}
	dsn, err := secretfile.Get("AML_CONTROL_DATABASE_URL", "")
	if err != nil {
		return err
	}
	if dsn == "" {
		return fmt.Errorf("AML_CONTROL_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.OpenPool(ctx, dsn)
	if err != nil {
		return fmt.Errorf("control-plane database unavailable")
	}
	defer pool.Close()
	var platformSchema, cryptoSchema int
	var enrolled bool
	if err = pool.QueryRow(ctx, cryptoschema.VersionQuery).Scan(&platformSchema, &cryptoSchema); err != nil || !cryptoschema.Compatible(platformSchema, cryptoSchema) {
		return fmt.Errorf("control-plane schema version mismatch")
	}
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database() AND vault_namespace=$1)`, namespace).Scan(&enrolled); err != nil || !enrolled {
		return fmt.Errorf("database has no matching immutable Vault namespace binding")
	}
	var network id.ID
	if err = pool.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE code=$1`, *networkCode).Scan(&network); err != nil {
		return fmt.Errorf("network not found")
	}
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	config, err := crypto_repo.NewAMLRepository().ConfigureAMLTestnet(ctx, aml.ConfigureTestnetRequest{AccountID: account, MerchantID: merchant, NetworkID: network, Config: aml.EmulatorConfig{Seed: *seed, MaxDelaySeconds: *delay, UnknownRateBP: *unknown}, Actor: *actor})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Provider  string `json:"provider"`
		AccountID id.ID  `json:"accountId"`
		NetworkID id.ID  `json:"networkId"`
		Seed      string `json:"seed"`
	}{aml.TestnetProvider, account, network, config.Seed})
}

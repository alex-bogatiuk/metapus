// Command aml-testnet-bootstrap plans or applies synthetic opening inventory.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/tenant"
	"metapus/internal/core/version"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/platform/secretfile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	namespaceFlag := flag.String("tenant-id", "", "Enrolled tenant UUID")
	idFlag := flag.String("request-id", "", "Stable bootstrap UUID")
	merchantFlag := flag.String("merchant-id", "", "Single historical merchant UUID")
	networkCode := flag.String("network-code", "", "Nile/Sepolia catalog code")
	height := flag.Int64("block-number", -1, "Canonical historical cutoff height")
	hash := flag.String("block-hash", "", "Canonical cutoff block hash")
	actor := flag.String("actor", "", "Operator identity")
	ttl := flag.Int("valid-days", 90, "Synthetic opening assessment lifetime, 1..365 days")
	manifest := flag.String("apply-manifest", "", "Apply an exact previously reviewed JSON manifest; omitted is read-only plan")
	flag.Parse()
	namespace, err := id.Parse(*namespaceFlag)
	if err != nil || id.IsNil(namespace) {
		return fmt.Errorf("tenant UUID required")
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
	var schema int
	var enrolled bool
	if err = pool.QueryRow(ctx, `SELECT COALESCE(MAX(version_id),0) FROM goose_db_version WHERE is_applied AND version_id<$1`, version.FirstExtensionMigration).Scan(&schema); err != nil || !version.CompatibleSchema(schema) {
		return fmt.Errorf("control-plane schema version mismatch")
	}
	if err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database() AND vault_namespace=$1)`, namespace).Scan(&enrolled); err != nil || !enrolled {
		return fmt.Errorf("matching database/Vault namespace enrollment required")
	}
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	repo := crypto_repo.NewAMLRepository()
	if *manifest != "" {
		info, err := os.Stat(*manifest)
		if err != nil || info.Size() > 2<<20 {
			return fmt.Errorf("bounded bootstrap manifest required")
		}
		raw, err := os.ReadFile(*manifest)
		if err != nil {
			return fmt.Errorf("cannot read bootstrap manifest")
		}
		var plan aml.TestnetBootstrap
		if err = json.UnmarshalStrict(raw, &plan); err != nil {
			return fmt.Errorf("invalid bootstrap manifest: %w", err)
		}
		if err = repo.ApplyAMLTestnetBootstrap(ctx, plan); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(struct {
			ID      id.ID `json:"id"`
			Applied bool  `json:"applied"`
		}{plan.ID, true})
	}
	requestID, err := id.Parse(*idFlag)
	if err != nil || id.IsNil(requestID) {
		return fmt.Errorf("stable request UUID required")
	}
	merchant, err := id.Parse(*merchantFlag)
	if err != nil || id.IsNil(merchant) {
		return fmt.Errorf("merchant UUID required")
	}
	if *ttl < 1 || *ttl > 365 {
		return fmt.Errorf("valid-days must be between 1 and 365")
	}
	var network id.ID
	if err = pool.QueryRow(ctx, `SELECT id FROM cat_blockchain_networks WHERE code=$1`, *networkCode).Scan(&network); err != nil {
		return fmt.Errorf("network not found")
	}
	plan, err := repo.PlanAMLTestnetBootstrap(ctx, aml.TestnetBootstrap{Schema: "aml-testnet-bootstrap.v1", ID: requestID, MerchantID: merchant, NetworkID: network, BlockNumber: *height, BlockHash: *hash, Actor: *actor, ExpiresAt: time.Now().UTC().Add(time.Duration(*ttl) * 24 * time.Hour)})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(plan)
}

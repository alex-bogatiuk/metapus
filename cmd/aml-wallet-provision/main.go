// Command aml-wallet-provision derives and enrolls one immutable merchant
// wallet through a dedicated control-plane Vault proxy before SQL publication.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/crypto/merchant_provisioning"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/vaultclient"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	requestID := flag.String("request-id", "", "Stable UUID, reused on retry")
	merchantID := flag.String("merchant-id", "", "Immutable merchant UUID")
	networkID := flag.String("network-id", "", "Network UUID")
	tenantID := flag.String("tenant-id", "", "Tenant UUID matching this database and Vault namespace")
	role := flag.String("role", "", "merchant_pool, liquidity_hot or network_fee")
	actor := flag.String("actor", "", "Audited control-plane operator identity")
	enrollDatabase := flag.Bool("enroll-database", false, "First enrollment of immutable database/Vault namespace binding")
	flag.Parse()
	ids := make([]id.ID, 4)
	for i, value := range []string{*requestID, *merchantID, *networkID, *tenantID} {
		parsed, err := id.Parse(value)
		if err != nil || id.IsNil(parsed) {
			return fmt.Errorf("request, merchant, network and tenant UUIDs required")
		}
		ids[i] = parsed
	}
	request := crypto.MerchantWalletRequest{ID: ids[0], MerchantID: ids[1], NetworkID: ids[2], Role: *role, Actor: *actor}
	if err := request.Validate(); err != nil {
		return err
	}
	dsn := os.Getenv("AML_CONTROL_DATABASE_URL")
	addr := os.Getenv("AML_CONTROL_VAULT_ADDR")
	if dsn == "" || addr == "" {
		return fmt.Errorf("AML_CONTROL_DATABASE_URL and dedicated AML_CONTROL_VAULT_ADDR are required")
	}
	pool, err := postgres.OpenPool(ctx, dsn)
	if err != nil {
		return fmt.Errorf("control-plane database unavailable")
	}
	defer pool.Close()
	if *enrollDatabase {
		if _, err = pool.Exec(ctx, `INSERT INTO sys_crypto_aml_provisioning_identity(vault_namespace,database_name,enrolled_by) VALUES($1,current_database(),$2) ON CONFLICT DO NOTHING`, ids[3], *actor); err != nil {
			return fmt.Errorf("cannot enroll provisioning database identity")
		}
	}
	var databaseTenant id.ID
	if err = pool.QueryRow(ctx, `SELECT vault_namespace FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database()`).Scan(&databaseTenant); err != nil || databaseTenant != ids[3] {
		return fmt.Errorf("database has no matching immutable Vault namespace binding")
	}
	client, err := vaultclient.New(vaultclient.Config{Addr: addr})
	if err != nil {
		return err
	}
	provisioner, err := merchant_provisioning.New(client, ids[3])
	if err != nil {
		return err
	}
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	walletID, err := cryptoapp.NewMerchantWalletProvisioningService(crypto_repo.NewAMLRepository(), provisioner).Run(ctx, request, "aml-control-"+id.New().String())
	if err != nil {
		return err
	}
	fmt.Println(walletID.String())
	return nil
}

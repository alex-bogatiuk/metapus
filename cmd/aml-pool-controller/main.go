// Command aml-pool-controller consumes finite merchant pool intents through a
// dedicated control-plane proxy. It never signs or broadcasts transactions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/infrastructure/crypto/merchant_provisioning"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/cryptoschema"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/platform/secretfile"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	namespaceFlag := flag.String("tenant-id", "", "Enrolled Vault namespace UUID")
	once := flag.Bool("once", false, "Run one bounded planning/provisioning cycle")
	renewAuthorization := flag.String("renew-exact-sweep", "", "Operator-authorize one fresh control signature for exact saved sweep bytes/nonce")
	retryJob := flag.String("retry-preflight", "", "Authorize a new finite RPC cycle for one exhausted empty-wallet preflight")
	retryReason := flag.String("reason", "", "Operator investigation and retry reason")
	retryActor := flag.String("actor", "", "Operator identity for the retry audit")
	batchPath := flag.String("configure-batch", "", "Apply one batch policy manifest and exit; no Vault calls")
	manifestPath := flag.String("configure", "", "Apply one local policy manifest and exit; no Vault calls")
	flag.Parse()
	namespace, err := id.Parse(*namespaceFlag)
	if err != nil || id.IsNil(namespace) {
		return fmt.Errorf("canonical tenant UUID required")
	}
	dsn, err := secretfile.Get("AML_CONTROL_DATABASE_URL", "")
	if err != nil {
		return err
	}
	addr := os.Getenv("AML_CONTROL_VAULT_ADDR")
	if (*manifestPath != "" && *batchPath != "") || (*retryJob != "" && (*manifestPath != "" || *batchPath != "")) || (*renewAuthorization != "" && (*retryJob != "" || *manifestPath != "" || *batchPath != "")) {
		return fmt.Errorf("choose one policy manifest")
	}
	if dsn == "" || (addr == "" && *manifestPath == "" && *batchPath == "" && *retryJob == "" && *renewAuthorization == "") {
		return fmt.Errorf("AML_CONTROL_DATABASE_URL and dedicated AML_CONTROL_VAULT_ADDR required")
	}
	pool, err := postgres.OpenPool(ctx, dsn)
	if err != nil {
		return fmt.Errorf("control-plane database unavailable")
	}
	defer pool.Close()
	var enrolled id.ID
	var platformSchema, cryptoSchema int
	if err = pool.QueryRow(ctx, `SELECT vault_namespace FROM sys_crypto_aml_provisioning_identity WHERE id AND database_name=current_database()`).Scan(&enrolled); err != nil || enrolled != namespace {
		return fmt.Errorf("database has no matching immutable Vault namespace binding")
	}
	if err = pool.QueryRow(ctx, cryptoschema.VersionQuery).Scan(&platformSchema, &cryptoSchema); err != nil || !cryptoschema.Compatible(platformSchema, cryptoSchema) {
		return fmt.Errorf("control-plane schema version mismatch")
	}
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManagerFromRawPool(pool))
	repo := crypto_repo.NewAMLRepository()
	if *renewAuthorization != "" {
		authorization, err := id.Parse(*renewAuthorization)
		if err != nil {
			return fmt.Errorf("authorization row UUID required")
		}
		renewal, err := repo.AuthorizeExactSweepRenewal(ctx, authorization, *retryReason, *retryActor)
		if err != nil {
			return err
		}
		fmt.Println("Exact saved-envelope renewal authorized:", renewal.String())
		return nil
	}
	if *retryJob != "" {
		job, err := id.Parse(*retryJob)
		if err != nil {
			return fmt.Errorf("preflight job UUID required")
		}
		if err = repo.RetryExhaustedAMLPreflight(ctx, job, *retryReason, *retryActor); err != nil {
			return err
		}
		fmt.Println("One finite preflight retry cycle authorized; observations and holds preserved")
		return nil
	}
	if *batchPath != "" {
		request, err := readBatchPolicyManifest(*batchPath)
		if err != nil {
			return err
		}
		version, err := repo.ConfigureAMLBatchPolicy(ctx, request)
		if err != nil {
			return err
		}
		fmt.Printf("batch-policy class=%s version=%d\n", request.Class, version)
		return nil
	}
	if *manifestPath != "" {
		request, err := readPolicyManifest(*manifestPath)
		if err != nil {
			return err
		}
		policy, err := repo.ConfigureMerchantPoolPolicy(ctx, request)
		if err != nil {
			return err
		}
		fmt.Printf("pool=%s version=%d enabled=%t\n", policy.ID.String(), policy.Version, policy.Enabled)
		return nil
	}
	client, err := vaultclient.New(vaultclient.Config{Addr: addr})
	if err != nil {
		return err
	}
	vault, err := merchant_provisioning.New(client, namespace)
	if err != nil {
		return err
	}
	service := cryptoapp.NewMerchantWalletProvisioningService(repo, vault)
	worker := "aml-pool-" + id.New().String()
	for {
		result := runCycle(ctx, repo, service, worker)
		if result.Errors > 0 {
			fmt.Fprintln(os.Stderr, "merchant pool cycle has deferred failures:", result.Errors)
		}
		if *once {
			fmt.Printf("planned=%d opened=%d provisioned=%d deferred=%d\n", result.Planned, result.Opened, result.Provisioned, result.Errors)
			if result.Errors > 0 {
				return fmt.Errorf("merchant pool cycle incomplete")
			}
			return nil
		}
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type cycleResult struct{ Planned, Opened, Provisioned, Errors int }

func runCycle(ctx context.Context, repo *crypto_repo.AMLRepository, service *cryptoapp.MerchantWalletProvisioningService, worker string) cycleResult {
	var result cycleResult
	for range 10 {
		if ctx.Err() != nil {
			return result
		}
		progressed, created, err := repo.ReconcileNextMerchantPool(ctx)
		if err != nil {
			result.Errors++
			fmt.Fprintln(os.Stderr, "merchant pool planning failed:", err)
		}
		result.Planned += created
		if !progressed {
			break
		}
	}
	for range 10 {
		if ctx.Err() != nil {
			return result
		}
		progressed, err := repo.ReleaseNextMerchantPoolBaselineHold(ctx)
		if err != nil {
			result.Errors++
			fmt.Fprintln(os.Stderr, "merchant pool baseline preflight failed:", err)
		}
		if !progressed {
			break
		}
	}
	for range 10 {
		if ctx.Err() != nil {
			return result
		}
		progressed, err := repo.OpenNextReadyMerchantPoolBatch(ctx)
		if err != nil {
			result.Errors++
			fmt.Fprintln(os.Stderr, "merchant pool batch opening failed:", err)
		} else if progressed {
			result.Opened++
		}
		if !progressed {
			break
		}
	}
	consecutiveErrors := 0
	for range 10 {
		if ctx.Err() != nil {
			return result
		}
		claimed, _, err := service.RunNext(ctx, worker)
		if err != nil {
			result.Errors++
			fmt.Fprintln(os.Stderr, "merchant pool provisioning failed:", err)
			consecutiveErrors++
		} else if claimed {
			result.Provisioned++
			consecutiveErrors = 0
		}
		if !claimed || consecutiveErrors >= 3 {
			break
		}
	}
	return result
}

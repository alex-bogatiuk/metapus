package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	chainbootstrap "github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/bootstrap"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/crypto_worker"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
	"github.com/alex-bogatiuk/metapus/internal/platform/secretfile"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "balance", "read-only mode: aml, balance, ingestion, rpc or evidence")
	txHash := flag.String("tx", "", "transaction hash for read-only evidence diagnostics")
	samples := flag.Int("samples", 5, "RPC check samples per recent/historical range (1..10)")
	historicalBlock := flag.Int64("historical-block", 0, "optional historical RPC check sample height")
	internalTransferTx := flag.String("internal-transfer-tx", "", "public transaction hash containing a positive internal TRX transfer")
	flag.Parse()
	switch *mode {
	case "aml", "balance", "ingestion", "rpc", "evidence":
	default:
		return fmt.Errorf("unsupported preflight mode %q", *mode)
	}
	dsn, err := secretfile.Get("DATABASE_URL", "")
	if err != nil {
		return fmt.Errorf("load DATABASE_URL: %w", err)
	}
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	poolCfg := postgres.DefaultPoolConfig(dsn)
	poolCfg.MaxConns = 4
	poolCfg.MinConns = 1
	pool, err := postgres.NewPool(ctx, poolCfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	if *mode == "evidence" {
		return runTransferEvidence(ctx, pool, *txHash)
	}
	if *mode == "aml" {
		ctx = tenant.WithTxManager(ctx, postgres.NewTxManager(pool))
		preflight, err := crypto_worker.NewAMLConfigurationPreflight(preflightSignerConfig())
		if err != nil {
			return err
		}
		result, err := preflight.Run(ctx, os.Getenv("CRYPTO_PREFLIGHT_NETWORK_CODE"))
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			return encodeErr
		}
		if err != nil {
			return err
		}
		if !result.Ready {
			return fmt.Errorf("AML configuration preflight is blocked; inspect reported blockers")
		}
		return nil
	}

	driverBuilder, err := chainbootstrap.NewBuilder(os.Getenv("CRYPTO_CHAIN_CONFIG_FILE"), os.Getenv("APP_ENV"))
	if err != nil {
		return fmt.Errorf("load crypto chain runtime configuration: %w", err)
	}

	txm := postgres.NewTxManager(pool)
	ctx = tenantdb.WithPool(ctx, pool.Unwrap())
	ctx = tenant.WithTxManager(ctx, txm)

	processorConfig := preflightProcessorConfig()
	processorConfig.DriverRegistryBuilder = driverBuilder
	processor := crypto_worker.NewCryptoProcessor(processorConfig, logger.Default())
	preflightNetwork := os.Getenv("CRYPTO_PREFLIGHT_NETWORK_CODE")
	if *mode == "rpc" {
		result, err := processor.RunIngestionRPCCheck(ctx, preflightNetwork, crypto.IngestionRPCCheckOptions{Samples: *samples, HistoricalBlock: *historicalBlock, InternalTransferTxHash: *internalTransferTx})
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			return encodeErr
		}
		if err != nil {
			return err
		}
		if !result.Ready {
			return fmt.Errorf("RPC check has insufficient operation/role coverage")
		}
		return nil
	}
	if *mode == "ingestion" {
		result, err := processor.RunIngestionPreflight(ctx, preflightNetwork)
		if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
			return encodeErr
		}
		if err != nil {
			return err
		}
		if !result.Ready {
			return fmt.Errorf("crypto ingestion preflight is not ready")
		}
		return nil
	}
	if preflightNetwork != "" {
		err = processor.InitializeNetworkPreflight(ctx, preflightNetwork)
	} else {
		err = processor.InitializeChainRuntime(ctx)
	}
	if err != nil {
		return fmt.Errorf("initialize crypto chain runtime: %w", err)
	}
	var result crypto_worker.ReconciliationResult
	if preflightNetwork != "" {
		result, err = processor.RunNetworkBalanceReconciliation(ctx, preflightNetwork)
	} else {
		result, err = processor.RunBalanceReconciliation(ctx)
	}
	if err != nil {
		return fmt.Errorf("run crypto pool preflight: %w", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return err
	}
	if err := validatePreflightResult(result); err != nil {
		return err
	}
	return validatePreflightIncidents(ctx, txm.GetQuerier(ctx), preflightNetwork)
}

func validatePreflightResult(result crypto_worker.ReconciliationResult) error {
	if !result.Acquired {
		return fmt.Errorf("crypto pool preflight skipped: another reconciliation worker owns the lease")
	}
	if result.ReadErrors != 0 {
		return fmt.Errorf("crypto pool preflight completed with %d chain read errors", result.ReadErrors)
	}
	if result.AttemptedPairs != result.EligiblePairs || result.WalletsChecked != result.EligiblePairs {
		return fmt.Errorf("crypto pool preflight coverage incomplete: eligible=%d attempted=%d successful=%d",
			result.EligiblePairs, result.AttemptedPairs, result.WalletsChecked)
	}
	return nil
}

func validatePreflightIncidents(ctx context.Context, q postgres.Querier, networkCode string) error {
	var open int
	err := q.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reg_crypto_reconciliation_items i
		JOIN cat_blockchain_networks n ON n.id=i.network_id
		WHERE i.kind IN ('token_balance_mismatch','native_fee_balance_mismatch')
		  AND i.status IN ('open','acknowledged')
		  AND ($1='' OR n.code=$1)
	`, networkCode).Scan(&open)
	if err != nil {
		return fmt.Errorf("check open crypto balance incidents: %w", err)
	}
	if open != 0 {
		return fmt.Errorf("crypto pool preflight has %d open balance reconciliation incidents", open)
	}
	return nil
}

func configValue(name string) string {
	value, err := secretfile.Get(name, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load %s: %v\n", name, err)
		os.Exit(1)
	}
	return value
}

func preflightSignerConfig() crypto_worker.CryptoProcessorConfig {
	cfg := crypto_worker.CryptoProcessorConfig{
		AppEnv: os.Getenv("APP_ENV"), VaultAddr: os.Getenv("VAULT_ADDR"), VaultToken: configValue("VAULT_TOKEN"),
		AuthorizationSigner: os.Getenv("CRYPTO_AUTH_SIGNER"), TransitMount: os.Getenv("CRYPTO_TRANSIT_MOUNT"), TenantID: os.Getenv("TENANT_ID"),
	}
	if strings.EqualFold(strings.TrimSpace(cfg.AuthorizationSigner), "local") {
		cfg.LocalAuthorizationKID = os.Getenv("CRYPTO_LOCAL_AUTH_KID")
		cfg.SigningAuthorizationPrivateKey = configValue("CRYPTO_SIGNING_AUTH_PRIVATE_KEY")
	}
	return cfg
}

func preflightProcessorConfig() crypto_worker.CryptoProcessorConfig {
	cfg := preflightSignerConfig()
	cfg.ApprovalPolicyPublicKey = configValue("CRYPTO_APPROVAL_POLICY_PUBLIC_KEY")
	cfg.IntentSigningPublicKey = configValue("CRYPTO_INTENT_SIGNING_PUBLIC_KEY")
	cfg.RiskAssessmentSigningPublicKey = configValue("CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY")
	cfg.PolicyDecisionSigningPublicKey = configValue("CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY")
	cfg.AdminApprovalSigningPublicKey = configValue("CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY")
	cfg.RiskApprovalSigningPublicKey = configValue("CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY")
	return cfg
}

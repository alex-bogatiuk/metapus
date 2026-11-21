package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	chainbootstrap "metapus/internal/configuration/crypto/infrastructure/blockchain/bootstrap"
	"metapus/internal/configuration/crypto/infrastructure/crypto_worker"
	"metapus/internal/configuration/crypto/infrastructure/ingestionmonitor"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/id"
	"metapus/internal/core/tenant"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/platform/secretfile"
	"metapus/internal/platform/telemetry"
	"metapus/pkg/logger"
)

var Version = "dev"
var BuildTime = "unknown"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	tenantID := os.Getenv("TENANT_ID")
	if _, err := id.Parse(tenantID); err != nil {
		return fmt.Errorf("valid TENANT_ID is required")
	}
	dsn, err := secretfile.Get("DATABASE_URL", "")
	if err != nil {
		return err
	}
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdown, err := telemetry.ConfigureMetrics(ctx, "crypto-ingestion-monitor", Version, os.Getenv)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()
	poolCfg := postgres.DefaultPoolConfig(dsn)
	poolCfg.MaxConns = 4
	poolCfg.MinConns = 1
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := postgres.NewPool(connectCtx, poolCfg)
	cancel()
	if err != nil {
		return fmt.Errorf("initialize ingestion monitor database: %w", err)
	}
	defer pool.Close()
	ctx = tenantdb.WithPool(ctx, pool.Unwrap())
	ctx = tenant.WithTxManager(ctx, postgres.NewTxManager(pool))
	interval, err := durationEnv("CRYPTO_INGESTION_MONITOR_INTERVAL", 30*time.Second, 5*time.Second, 5*time.Minute)
	if err != nil {
		return err
	}
	policy := cryptoapp.DefaultIngestionAlertPolicy()
	// Adapter read budgets are capped at 60 s. Networks/stream heads are read
	// independently; leave time for DB reads and a bounded recovery probe.
	sampleTimeout, err := durationEnv("CRYPTO_INGESTION_MONITOR_TIMEOUT", 75*time.Second, 5*time.Second, 3*time.Minute)
	if err != nil {
		return err
	}
	policy.MaxSampleGap = max(3*interval, 2*sampleTimeout)
	builder, err := chainbootstrap.NewBuilder(os.Getenv("CRYPTO_CHAIN_CONFIG_FILE"), os.Getenv("APP_ENV"))
	if err != nil {
		return fmt.Errorf("initialize ingestion monitor configuration: %w", err)
	}
	processor := crypto_worker.NewCryptoProcessor(crypto_worker.CryptoProcessorConfig{
		AppEnv:                         os.Getenv("APP_ENV"),
		TenantID:                       tenantID,
		DriverRegistryBuilder:          builder,
		ApprovalPolicyPublicKey:        os.Getenv("CRYPTO_APPROVAL_POLICY_PUBLIC_KEY"),
		IntentSigningPublicKey:         os.Getenv("CRYPTO_INTENT_SIGNING_PUBLIC_KEY"),
		RiskAssessmentSigningPublicKey: os.Getenv("CRYPTO_RISK_ASSESSMENT_SIGNING_PUBLIC_KEY"),
		PolicyDecisionSigningPublicKey: os.Getenv("CRYPTO_POLICY_DECISION_SIGNING_PUBLIC_KEY"),
		AdminApprovalSigningPublicKey:  os.Getenv("CRYPTO_ADMIN_APPROVAL_SIGNING_PUBLIC_KEY"),
		RiskApprovalSigningPublicKey:   os.Getenv("CRYPTO_RISK_APPROVAL_SIGNING_PUBLIC_KEY"),
	}, logger.Default())
	observer := processor.NewIngestionObserver(os.Getenv("CRYPTO_INGESTION_MONITOR_NETWORK_CODE"))
	defer observer.Close()
	dir := os.Getenv("CRYPTO_INGESTION_MONITOR_STATE_DIR")
	if dir == "" {
		dir = "/var/lib/metapus/ingestion-monitor"
	}
	history, err := ingestionmonitor.OpenHistory(dir, tenantID)
	if err != nil {
		return err
	}
	defer func() { _ = history.Close() }()
	address := os.Getenv("CRYPTO_INGESTION_MONITOR_ADDRESS")
	if address == "" {
		address = ":8092"
	}
	runtime := ingestionmonitor.Runtime{Observer: observer, History: history, Deliver: crypto_repo.DeliverIngestionAlert, Policy: policy, Interval: interval, SampleTimeout: sampleTimeout, Address: address, Metrics: crypto_worker.NewIngestionMonitorMetrics()}
	logger.Info(ctx, "ingestion monitor started", "version", Version, "build_time", BuildTime, "interval_seconds", interval.Seconds())
	return runtime.Run(ctx)
}

func durationEnv(name string, fallback, min, max time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < min || duration > max {
		return 0, fmt.Errorf("%s must be a duration between %s and %s", name, min, max)
	}
	return duration, nil
}

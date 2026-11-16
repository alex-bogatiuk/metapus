// Command integration-test runs the complete database suite against a fresh,
// uniquely named database in an explicitly configured test cluster.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/extensions"
	"metapus/internal/content"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres/migration"
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
	adminDSN := os.Getenv("METAPUS_TEST_ADMIN_DSN")
	if adminDSN == "" {
		adminDSN = os.Getenv("TENANT_POSTGRES_TEST_URL")
	}
	if adminDSN == "" {
		return errors.New("METAPUS_TEST_ADMIN_DSN (or TENANT_POSTGRES_TEST_URL) must identify a disposable PostgreSQL test cluster")
	}
	target, err := url.Parse(adminDSN)
	if err != nil || (target.Scheme != "postgres" && target.Scheme != "postgresql") {
		return errors.New("test cluster DSN must be a postgres:// or postgresql:// URL")
	}
	cfg, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		return errors.New("invalid test cluster DSN")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(probeCtx, cfg)
	if err != nil {
		return errors.New("test PostgreSQL cluster is unavailable")
	}
	defer func() { _ = conn.Close(context.Background()) }()
	name := "metapus_integration_" + id.Compact(id.New())
	if _, err := conn.Exec(probeCtx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return fmt.Errorf("create isolated integration database: %w", err)
	}
	// Leave the generated database for failed-test inspection. The caller owns
	// the disposable cluster; this runner never drops or resets an input DB.
	target.Path = "/" + name
	dsn := target.String()
	env := integrationEnv(os.Environ(), adminDSN, dsn)
	// The core rollback suite must own an empty database: extension versions
	// share goose's version table but are not part of the core provider.
	migrationName := "metapus_migrations_" + id.Compact(id.New())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{migrationName}.Sanitize()); err != nil {
		return fmt.Errorf("create isolated migration database: %w", err)
	}
	migrationTarget := *target
	migrationTarget.Path = "/" + migrationName
	for i := range env {
		if strings.HasPrefix(env[i], "METAPUS_TEST_MIGRATION_DSN=") {
			env[i] = "METAPUS_TEST_MIGRATION_DSN=" + migrationTarget.String()
		}
	}

	list := exec.CommandContext(ctx, "go", "test", "-tags=integration", "-list", "^Test", "./...")
	list.Env = env
	output, err := list.CombinedOutput()
	if err != nil {
		return fmt.Errorf("list integration tests: %w\n%s", err, output)
	}
	for _, test := range []string{
		"TestRunAllFreshCryptoLifecycleSchema",
		"TestWithdrawalRecoveryReusesSignedAttemptAfterBroadcastBeforePostFailure",
		"TestNonceReservationIsConcurrentIdempotentAndReclaimsOnlyUnsignedTail",
		"TestWithdrawalRecoveryFinalityCompletesOnceWithTwoWorkers",
		"TestRefreshCleanupPreservesRecentRevocationsIncludingExpiredTokens",
		"TestAuthorizedSupplementalReadMatrixAcrossTenants",
		"TestCatalogPresenterMasksEveryResponseAndReauthorizesReplay",
		"TestCELSQLMatchesGoPolicyEngineBeforeAggregation",
		"TestReportPoliciesApplyBeforeUnionAndAggregation",
		"TestBuiltInJournalUsesFreshCryptoSchema",
		"TestHierarchyWritersSerializeMovesAndRejectExistingCycles",
		"TestCatalogAuditAndOutboxCommitAtomically",
		"TestTransactionAfterCommitTracksSavepointOutcome",
		"TestNullableKeysetEveryDirectionAndExactBoundary",
		"TestFreshSchemaAuthNamesUUIDDefaultsAndTenantBindingExceptions",
		"TestDeclaredTablePartsMatchSchema",
		"TestTenantPoolLifecycleConcurrentCreationReferencesAndShutdown",
		"TestIntegrationAccountsPersistQuotaAndConcurrentSelection",
		"TestRPCBindingPicksUpTenantChangesWithoutRestart",
	} {
		if !strings.Contains("\n"+string(output), "\n"+test+"\n") && !strings.Contains("\n"+string(output), "\n"+test+"\r\n") {
			return fmt.Errorf("integration build omitted required test %s", test)
		}
	}
	// Verify the target before applying any migration.
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	var actual string
	if err := db.QueryRowContext(ctx, "SELECT current_database()").Scan(&actual); err != nil {
		return err
	}
	if actual != name {
		return errors.New("integration database identity mismatch")
	}
	if output, err := migration.RunAll(dsn, migration.WithPermissions(content.Permissions(extensions.Register))); err != nil {
		return fmt.Errorf("prepare integration schema: %w\n%s", err, output)
	}
	fmt.Println("Integration database:", name)
	evidenceDir := os.Getenv("METAPUS_TEST_EVIDENCE_DIR")
	if evidenceDir != "" {
		if err := os.MkdirAll(evidenceDir, 0755); err != nil {
			return err
		}
		if err := financialSnapshot(ctx, env, evidenceDir, "before"); err != nil {
			return err
		}
	}
	args := []string{"-tags=integration", "-race", "-count=1", "-p=1", "./..."}
	cmd := exec.CommandContext(ctx, "go", append([]string{"test"}, args...)...)
	if junit := os.Getenv("METAPUS_TEST_JUNIT_FILE"); junit != "" {
		cmd = exec.CommandContext(ctx, "gotestsum", append([]string{"--junitfile", junit, "--format", "standard-verbose", "--"}, args...)...)
	}
	cmd.Env, cmd.Stdout, cmd.Stderr = env, os.Stdout, os.Stderr
	testErr := cmd.Run()
	if evidenceDir != "" {
		testErr = errors.Join(testErr, financialSnapshot(ctx, env, evidenceDir, "after"))
	}
	return testErr
}

// Evidence always describes the same generated DB that the test process used.
func financialSnapshot(ctx context.Context, env []string, directory, phase string) error {
	cmd := exec.CommandContext(ctx, "bash", "scripts/ci/write-crypto-financial-snapshot.sh", filepath.Join(directory, "full-gate-financial-"+phase+".json"), phase+"-full-gate")
	cmd.Env, cmd.Stdout, cmd.Stderr = env, os.Stdout, os.Stderr
	return cmd.Run()
}

func integrationEnv(original []string, adminDSN, dsn string) []string {
	values := map[string]string{
		"DATABASE_URL":                    dsn,
		"METAPUS_TEST_DATABASE_DSN":       dsn,
		"METAPUS_TEST_MIGRATION_DSN":      dsn,
		"METAPUS_TEST_MARKERS_DSN":        dsn,
		"METAPUS_TEST_EXCHANGE_RATES_DSN": dsn,
		"TENANT_POSTGRES_TEST_URL":        adminDSN,
		// Integration repository fixtures create and drop their own databases.
		"METAPUS_TEST_INTEGRATIONS_DSN": adminDSN,
	}
	env := make([]string, 0, len(original)+len(values))
	for _, value := range original {
		key, _, _ := strings.Cut(value, "=")
		if _, replaced := values[key]; !replaced {
			env = append(env, value)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

//go:build integration

package postgres_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"metapus/internal/core/tenant"
	"metapus/internal/core/version"
	postgresstore "metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/tenantdb"
)

func TestTenantProvisioningPostgres(t *testing.T) {
	adminDSN := os.Getenv("TENANT_POSTGRES_TEST_URL")
	if adminDSN == "" {
		t.Fatal("TENANT_POSTGRES_TEST_URL is required for integration tests")
	}

	repoRoot := integrationRepoRoot(t)
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	require.NoError(t, err)
	t.Cleanup(adminPool.Close)
	require.NoError(t, adminPool.Ping(ctx))

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	metaName := "mt_meta_test_" + suffix[:16]
	dbAdmin := postgresstore.NewTenantDatabaseAdmin(adminPool)
	quotedName := "mt-quoted-test-" + suffix[:12]
	quotedCreated, err := dbAdmin.EnsureDatabase(ctx, quotedName)
	require.NoError(t, err)
	require.True(t, quotedCreated)
	t.Cleanup(func() { dropTestDatabase(ctx, t, adminPool, quotedName) })

	created, err := dbAdmin.EnsureDatabase(ctx, metaName)
	require.NoError(t, err)
	require.True(t, created)

	var tenantDBName string
	var metaPool *pgxpool.Pool
	t.Cleanup(func() {
		if metaPool != nil {
			metaPool.Close()
		}
		if tenantDBName != "" {
			dropTestDatabase(ctx, t, adminPool, tenantDBName)
		}
		dropTestDatabase(ctx, t, adminPool, metaName)
	})

	metaDSN := replaceDatabaseInDSN(t, adminDSN, metaName)
	_, err = migration.RunMeta(metaDSN)
	require.NoError(t, err)

	metaPool, err = pgxpool.New(ctx, metaDSN)
	require.NoError(t, err)
	require.NoError(t, metaPool.Ping(ctx))

	parsed, err := url.Parse(adminDSN)
	require.NoError(t, err)
	password, _ := parsed.User.Password()
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)

	repo := tenantdb.NewPostgresRegistry(metaPool)
	migrator := &integrationMigrator{user: parsed.User.Username(), password: password}
	svc := tenant.NewProvisioner(repo, dbAdmin, migrator)
	input := tenant.CreateTenantInput{
		Slug:        "tenant-cli-e2e",
		DisplayName: "Tenant CLI E2E",
		DBHost:      parsed.Hostname(),
		DBPort:      port,
	}

	var wg sync.WaitGroup
	results := make(chan *tenant.ProvisionResult, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, provisionErr := svc.Provision(ctx, input)
			results <- result
			errs <- provisionErr
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for provisionErr := range errs {
		require.NoError(t, provisionErr)
	}
	for result := range results {
		require.NotNil(t, result)
		require.NotNil(t, result.Tenant)
		tenantDBName = result.Tenant.DBName
	}

	require.Equal(t, int32(1), migrator.calls.Load())
	require.Regexp(t, `^mt_[0-9a-f]{32}$`, tenantDBName)

	stored, err := repo.GetBySlug(ctx, "tenant-cli-e2e")
	require.NoError(t, err)
	require.Equal(t, tenant.StatusActive, stored.Status)
	require.Equal(t, version.ExpectedSchemaVersion, stored.SchemaVersion)
	require.Equal(t, tenant.ProvisioningStageComplete, stored.ProvisioningStage)
	require.Empty(t, stored.ProvisioningError)
	require.NotNil(t, stored.ProvisionedAt)

	tenantPool, err := pgxpool.New(ctx, tenantdb.DSN(stored, parsed.User.Username(), password))
	require.NoError(t, err)
	require.NoError(t, tenantPool.Ping(ctx))
	tenantPool.Close()
}

type integrationMigrator struct {
	user     string
	password string
	calls    atomic.Int32
}

func (m *integrationMigrator) Migrate(_ context.Context, t *tenant.Tenant) (int, error) {
	m.calls.Add(1)
	_, err := migration.RunAll(tenantdb.DSN(t, m.user, m.password))
	if err != nil {
		return 0, err
	}
	return version.ExpectedSchemaVersion, nil
}

func integrationRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

func replaceDatabaseInDSN(t *testing.T, dsn, databaseName string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	u.Path = "/" + databaseName
	return u.String()
}

func dropTestDatabase(ctx context.Context, t *testing.T, pool *pgxpool.Pool, databaseName string) {
	t.Helper()
	_, err := pool.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{databaseName}.Sanitize()+" WITH (FORCE)")
	require.NoError(t, err)
}

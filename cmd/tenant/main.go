// Package main provides CLI for tenant management.
// Usage: tenant create --slug acme --name "ACME Corp"
//
//	tenant list
//	tenant migrate --all
//	tenant suspend <tenant-id>
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/tenant"
	"metapus/internal/core/version"
	"metapus/internal/infrastructure/storage/postgres/migration"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx := context.Background()

	switch os.Args[1] {
	case "create":
		createTenant(ctx)
	case "list":
		listTenants(ctx)
	case "init-meta":
		initMeta(ctx)
	case "migrate":
		migrateTenants(ctx)
	case "promote":
		promoteTenant(ctx)
	case "suspend":
		suspendTenant(ctx)
	case "activate":
		activateTenant(ctx)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Metapus Tenant Management CLI

Usage:
  tenant <command> [options]

Commands:
  init-meta Initialize meta database structure
  create    Create a new tenant
  list      List all tenants
  migrate   Run migrations for tenant(s)
  promote   Assign tenant to a version group (cloud mode)
  suspend   Suspend a tenant
  activate  Activate a suspended tenant
  help      Show this help

Environment Variables:
  META_DATABASE_URL    Connection string for meta database (required)
  TENANT_DB_USER       Username for tenant databases (required)
  TENANT_DB_PASSWORD   Password for tenant databases (required)
  POSTGRES_ADMIN_URL   Admin connection for creating databases

Examples:
  tenant create --slug acme --name "ACME Corporation"
  tenant list
  tenant migrate --all
  tenant migrate --id <tenant-uuid>
  tenant promote --id <tenant-uuid> --to v1.3.0
  tenant suspend <tenant-uuid>
  tenant activate <tenant-uuid>`)
}

func getMetaPool(ctx context.Context) *pgxpool.Pool {
	metaDSN := os.Getenv("META_DATABASE_URL")
	if metaDSN == "" {
		metaDSN = "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"
		//fmt.Println("Error: META_DATABASE_URL environment variable is required")
		//os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, metaDSN)
	if err != nil {
		fmt.Printf("Error connecting to meta database: %v\n", err)
		os.Exit(1)
	}

	return pool
}

const metaSchemaSQL = `
CREATE TABLE IF NOT EXISTS tenants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            VARCHAR(63) NOT NULL,
    display_name    VARCHAR(255) NOT NULL,
    db_name         VARCHAR(63) NOT NULL UNIQUE,
    db_host         VARCHAR(255) NOT NULL DEFAULT 'localhost',
    db_port         INT NOT NULL DEFAULT 5432,
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    plan            VARCHAR(50) NOT NULL DEFAULT 'standard',
    schema_version  INT NOT NULL DEFAULT 0,
    version_group   VARCHAR(20) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settings        JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tenants_slug_lower ON tenants (lower(slug));
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenants_status_slug ON tenants(status, slug) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_tenants_version_group ON tenants(version_group, status) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS tenant_migrations (
    id          SERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    version     INT NOT NULL,
    name        VARCHAR(255) NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    checksum    VARCHAR(64),
    duration_ms INT,
    UNIQUE(tenant_id, version)
);
CREATE INDEX IF NOT EXISTS idx_tenant_migrations_tenant ON tenant_migrations(tenant_id);

CREATE TABLE IF NOT EXISTS tenant_audit (
    id          SERIAL PRIMARY KEY,
    tenant_id   UUID REFERENCES tenants(id) ON DELETE SET NULL,
    action      VARCHAR(50) NOT NULL,
    actor       VARCHAR(255),
    details     JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tenant_audit_tenant ON tenant_audit(tenant_id);
CREATE INDEX IF NOT EXISTS idx_tenant_audit_created ON tenant_audit(created_at DESC);

CREATE OR REPLACE FUNCTION update_tenant_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trigger_tenants_updated_at ON tenants;
CREATE TRIGGER trigger_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION update_tenant_timestamp();
`

func getAdminDSN() string {
	adminDSN := os.Getenv("POSTGRES_ADMIN_URL")
	if adminDSN != "" {
		return adminDSN
	}

	metaDSN := os.Getenv("META_DATABASE_URL")
	if metaDSN == "" {
		metaDSN = "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"
	}

	u, err := url.Parse(metaDSN)
	if err == nil {
		u.Path = "/postgres"
		return u.String()
	}
	return strings.Replace(metaDSN, "/metapus_meta", "/postgres", 1)
}

func initMeta(ctx context.Context) {
	// 1. Ensure the tenants database exists
	adminDSN := getAdminDSN()

	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		fmt.Printf("Error connecting to admin database: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Ensuring meta database exists...")
	// Extract target meta database name
	metaDSN := os.Getenv("META_DATABASE_URL")
	if metaDSN == "" {
		metaDSN = "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable"
	}
	u, _ := url.Parse(metaDSN)
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		dbName = "tenants"
	}

	_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		fmt.Printf("Failed to create meta database (might already exist): %v\n", err)
	}
	adminPool.Close()

	// 2. Connect to the meta DB and run schema
	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	fmt.Println("Initializing meta database schema...")
	_, err = metaPool.Exec(ctx, metaSchemaSQL)
	if err != nil {
		fmt.Printf("Error initializing meta database: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Meta database schema initialized successfully!")
}

func createTenant(ctx context.Context) {
	var slug, name, plan string

	// Parse arguments
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--slug":
			if i+1 < len(os.Args) {
				slug = os.Args[i+1]
				i++
			}
		case "--name":
			if i+1 < len(os.Args) {
				name = os.Args[i+1]
				i++
			}
		case "--plan":
			if i+1 < len(os.Args) {
				plan = os.Args[i+1]
				i++
			}
		}
	}

	if slug == "" || name == "" {
		fmt.Println("Error: --slug and --name are required")
		fmt.Println("Usage: tenant create --slug <slug> --name <name> [--plan standard|premium|enterprise]")
		os.Exit(1)
	}

	if plan == "" {
		plan = "standard"
	}

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenant.NewPostgresRegistry(metaPool)

	// Generate database name
	dbName := "mt_" + strings.ToLower(slug)

	fmt.Printf("Creating tenant '%s'...\n", slug)

	// 1. Create database
	adminDSN := getAdminDSN()

	{
		fmt.Printf("  Creating database %s...\n", dbName)
		adminPool, err := pgxpool.New(ctx, adminDSN)
		if err != nil {
			fmt.Printf("  Warning: Could not connect as admin: %v\n", err)
			fmt.Println("  You may need to create the database manually.")
		} else {
			defer adminPool.Close()
			_, err = adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
			if err != nil {
				if strings.Contains(err.Error(), "already exists") {
					fmt.Println("  Database already exists")
				} else {
					fmt.Printf("  Warning: Could not create database: %v\n", err)
				}
			} else {
				fmt.Println("  Database created")
			}
		}
	}

	// 2. Run migrations
	dbUser := os.Getenv("TENANT_DB_USER")
	dbPassword := os.Getenv("TENANT_DB_PASSWORD")
	if dbUser != "" && dbPassword != "" {
		fmt.Println("  Running migrations...")
		dbHost := getEnvDefault("TENANT_DB_HOST", "localhost")
		dbPort := getEnvDefault("TENANT_DB_PORT", "5432")
		tenantDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			dbUser, dbPassword, dbHost, dbPort, dbName)

		if err := runAllMigrations(tenantDSN); err != nil {
			fmt.Printf("  Warning: Migrations failed: %v\n", err)
			fmt.Println("  You may need to run migrations manually.")
		} else {
			fmt.Println("  Migrations completed")
		}
	}

	// 3. Register in meta database
	fmt.Println("  Registering tenant...")

	t := &tenant.Tenant{
		Slug:        slug,
		DisplayName: name,
		DBName:      dbName,
		DBHost:      getEnvDefault("TENANT_DB_HOST", "localhost"),
		DBPort:      getEnvIntDefault("TENANT_DB_PORT", 5432),
		Status:      tenant.StatusActive,
		Plan:        tenant.Plan(plan),
	}

	if err := registry.Create(ctx, t); err != nil {
		fmt.Printf("Error registering tenant: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✓ Tenant '%s' created successfully!\n", slug)
	fmt.Printf("  Tenant ID: %s\n", t.ID)
	fmt.Printf("  Database: %s\n", dbName)
	fmt.Printf("  Status: active\n")
	fmt.Printf("  Plan: %s\n", plan)
}

func listTenants(ctx context.Context) {
	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenant.NewPostgresRegistry(metaPool)
	tenants, err := registry.ListAll(ctx)
	if err != nil {
		fmt.Printf("Error listing tenants: %v\n", err)
		os.Exit(1)
	}

	if len(tenants) == 0 {
		fmt.Println("No tenants found")
		return
	}

	fmt.Printf("%-36s %-20s %-30s %-15s %-6s %-12s %-10s\n", "TENANT_ID", "SLUG", "NAME", "DATABASE", "SCHEMA", "VERSION_GRP", "STATUS")
	fmt.Println(strings.Repeat("-", 155))

	for _, t := range tenants {
		vg := t.VersionGroup
		if vg == "" {
			vg = "-"
		}
		fmt.Printf("%-36s %-20s %-30s %-15s %-6s %-12s %-10s\n",
			truncate(t.ID, 36),
			truncate(t.Slug, 20),
			truncate(t.DisplayName, 30),
			truncate(t.DBName, 15),
			strconv.Itoa(t.SchemaVersion),
			vg,
			t.Status,
		)
	}
}

func migrateTenants(ctx context.Context) {
	var targetID string
	var all bool

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--id":
			if i+1 < len(os.Args) {
				targetID = os.Args[i+1]
				i++
			}
		case "--all":
			all = true
		}
	}

	if !all && targetID == "" {
		fmt.Println("Error: specify --id <tenant-uuid> or --all")
		os.Exit(1)
	}

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenant.NewPostgresRegistry(metaPool)

	var tenants []*tenant.Tenant
	var err error

	if all {
		tenants, err = registry.ListActive(ctx)
	} else {
		t, e := registry.GetByID(ctx, targetID)
		if e != nil {
			fmt.Printf("Error: tenant '%s' not found\n", targetID)
			os.Exit(1)
		}
		tenants = []*tenant.Tenant{t}
		err = e
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	dbUser := os.Getenv("TENANT_DB_USER")
	dbPassword := os.Getenv("TENANT_DB_PASSWORD")

	if dbUser == "" || dbPassword == "" {
		fmt.Println("Error: TENANT_DB_USER and TENANT_DB_PASSWORD are required")
		os.Exit(1)
	}

	for _, t := range tenants {
		fmt.Printf("Migrating %s (%s)...\n", t.Slug, t.DBName)

		dsn := t.DSN(dbUser, dbPassword)
		if err := runAllMigrations(dsn); err != nil {
			fmt.Printf("  ✗ Failed: %v\n", err)
		} else {
			// Update schema_version in meta-database after successful migration.
			if svErr := registry.UpdateSchemaVersion(ctx, t.ID, version.ExpectedSchemaVersion); svErr != nil {
				fmt.Printf("  ⚠ Migrated but failed to update schema_version: %v\n", svErr)
			} else {
				fmt.Printf("  ✓ Done (schema_version=%d)\n", version.ExpectedSchemaVersion)
			}
		}
	}
}

func suspendTenant(ctx context.Context) {
	if len(os.Args) < 3 {
		fmt.Println("Usage: tenant suspend <tenant-uuid>")
		os.Exit(1)
	}

	tenantID := os.Args[2]

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenant.NewPostgresRegistry(metaPool)
	if err := registry.UpdateStatusByID(ctx, tenantID, tenant.StatusSuspended); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Tenant '%s' suspended\n", tenantID)
}

func activateTenant(ctx context.Context) {
	if len(os.Args) < 3 {
		fmt.Println("Usage: tenant activate <tenant-uuid>")
		os.Exit(1)
	}

	tenantID := os.Args[2]

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenant.NewPostgresRegistry(metaPool)
	if err := registry.UpdateStatusByID(ctx, tenantID, tenant.StatusActive); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Tenant '%s' activated\n", tenantID)
}

// promoteTenant assigns a tenant to a version group (cloud mode).
// Usage: tenant promote --id <uuid> --to <version_group>
func promoteTenant(ctx context.Context) {
	var targetID, targetGroup string

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--id":
			if i+1 < len(os.Args) {
				targetID = os.Args[i+1]
				i++
			}
		case "--to":
			if i+1 < len(os.Args) {
				targetGroup = os.Args[i+1]
				i++
			}
		}
	}

	if targetID == "" || targetGroup == "" {
		fmt.Println("Usage: tenant promote --id <tenant-uuid> --to <version_group>")
		fmt.Println("Example: tenant promote --id 550e8400-... --to v1.3.0")
		os.Exit(1)
	}

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenant.NewPostgresRegistry(metaPool)

	// Verify tenant exists
	t, err := registry.GetByID(ctx, targetID)
	if err != nil {
		fmt.Printf("Error: tenant '%s' not found: %v\n", targetID, err)
		os.Exit(1)
	}

	oldGroup := t.VersionGroup
	if oldGroup == "" {
		oldGroup = "(none)"
	}

	if err := registry.UpdateVersionGroup(ctx, targetID, targetGroup); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Tenant '%s' (%s) promoted: %s → %s\n", t.Slug, targetID, oldGroup, targetGroup)
	fmt.Println("  Note: requests for this tenant will now be served by the server instance")
	fmt.Printf("  running with VERSION_GROUP=%s\n", targetGroup)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// runAllMigrations delegates to the shared migration package.
func runAllMigrations(dsn string) error {
	output, err := migration.RunAll(dsn)
	if output != "" {
		fmt.Print(output)
	}
	return err
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvIntDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

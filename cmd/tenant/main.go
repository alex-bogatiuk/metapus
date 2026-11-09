// Package main provides CLI for tenant management.
// Usage: tenant create --slug acme --name "ACME Corp"
//
//	tenant list
//	tenant migrate --all
//	tenant suspend <tenant-id>
package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/tenant"
	"metapus/internal/core/version"
	postgresstore "metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/migration"
	"metapus/internal/infrastructure/tenantdb"
	"metapus/internal/platform/secretfile"
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
		if err := migrateTenants(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
			os.Exit(1)
		}
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
  tenant suspend <tenant-uuid>
  tenant activate <tenant-uuid>`)
}

func getMetaPool(ctx context.Context) *pgxpool.Pool {
	metaDSN := getEnvDefault("META_DATABASE_URL", "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable")

	pool, err := postgresstore.OpenPool(ctx, metaDSN)
	if err != nil {
		fmt.Printf("Error connecting to meta database: %v\n", err)
		os.Exit(1)
	}

	return pool
}

func getAdminDSN() string {
	adminDSN := getEnvDefault("POSTGRES_ADMIN_URL", "")
	if adminDSN != "" {
		return adminDSN
	}

	metaDSN := getEnvDefault("META_DATABASE_URL", "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable")

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

	adminPool, err := postgresstore.OpenPool(ctx, adminDSN)
	if err != nil {
		fmt.Printf("Error connecting to admin database: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Ensuring meta database exists...")
	// Extract target meta database name
	metaDSN := getEnvDefault("META_DATABASE_URL", "postgres://metapus:metapus@localhost:5432/tenants?sslmode=disable")
	u, _ := url.Parse(metaDSN)
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		dbName = "tenants"
	}

	dbAdmin := postgresstore.NewTenantDatabaseAdmin(adminPool)
	created, err := dbAdmin.EnsureDatabase(ctx, dbName)
	if err != nil {
		fmt.Printf("Error ensuring meta database: %v\n", err)
		os.Exit(1)
	}
	if created {
		fmt.Println("Meta database created")
	} else {
		fmt.Println("Meta database already exists")
	}
	adminPool.Close()

	// 2. Apply the meta schema from its single goose migration source.
	fmt.Println("Applying meta database migrations...")
	output, err := migration.RunMeta(metaDSN)
	if output != "" {
		fmt.Print(output)
	}
	if err != nil {
		fmt.Printf("Error migrating meta database: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Meta database schema initialized successfully")
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

	dbUser := getEnvDefault("TENANT_DB_USER", "")
	dbPassword := getEnvDefault("TENANT_DB_PASSWORD", "")
	if dbUser == "" || dbPassword == "" {
		fmt.Println("Error: TENANT_DB_USER and TENANT_DB_PASSWORD are required")
		os.Exit(1)
	}

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()
	adminPool, err := postgresstore.OpenPool(ctx, getAdminDSN())
	if err != nil {
		fmt.Printf("Error creating admin connection pool: %v\n", err)
		os.Exit(1)
	}
	defer adminPool.Close()

	registry := tenantdb.NewPostgresRegistry(metaPool)
	provisioner := tenant.NewProvisioner(
		registry,
		postgresstore.NewTenantDatabaseAdmin(adminPool),
		&cliTenantMigrator{user: dbUser, password: dbPassword},
	)

	fmt.Printf("Provisioning tenant '%s'...\n", slug)
	result, err := provisioner.Provision(ctx, tenant.CreateTenantInput{
		Slug:        slug,
		DisplayName: name,
		DBHost:      getEnvDefault("TENANT_DB_HOST", "localhost"),
		DBPort:      getEnvIntDefault("TENANT_DB_PORT", 5432),
		Plan:        tenant.Plan(plan),
	})
	if err != nil {
		fmt.Printf("Error provisioning tenant: %v\n", err)
		os.Exit(1)
	}

	if result.AlreadyProvisioned {
		fmt.Println("  Tenant is already provisioned")
	} else if result.DatabaseCreated {
		fmt.Println("  Database created and migrations completed")
	} else {
		fmt.Println("  Existing database verified and migrations completed")
	}
	t := result.Tenant
	dbName := t.DBName

	fmt.Printf("\nTenant '%s' is ready\n", t.Slug)
	fmt.Printf("  Tenant ID: %s\n", t.ID)
	fmt.Printf("  Database: %s\n", dbName)
	fmt.Printf("  Status: active\n")
	fmt.Printf("  Plan: %s\n", plan)
}

type cliTenantMigrator struct {
	user     string
	password string
}

func (m *cliTenantMigrator) Migrate(_ context.Context, t *tenant.Tenant) (int, error) {
	fmt.Println("  Running migrations...")
	if err := runAllMigrations(tenantdb.DSN(t, m.user, m.password)); err != nil {
		return 0, err
	}
	return version.ExpectedSchemaVersion, nil
}

func listTenants(ctx context.Context) {
	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenantdb.NewPostgresRegistry(metaPool)
	tenants, err := registry.ListAll(ctx)
	if err != nil {
		fmt.Printf("Error listing tenants: %v\n", err)
		os.Exit(1)
	}

	if len(tenants) == 0 {
		fmt.Println("No tenants found")
		return
	}

	fmt.Printf("%-36s %-20s %-30s %-15s %-6s %-16s\n", "TENANT_ID", "SLUG", "NAME", "DATABASE", "SCHEMA", "STATUS")
	fmt.Println(strings.Repeat("-", 130))

	for _, t := range tenants {
		fmt.Printf("%-36s %-20s %-30s %-15s %-6s %-16s\n",
			truncate(t.ID, 36),
			truncate(t.Slug, 20),
			truncate(t.DisplayName, 30),
			truncate(t.DBName, 15),
			strconv.Itoa(t.SchemaVersion),
			t.Status,
		)
	}
}

func migrateTenants(ctx context.Context) error {
	targetID, all, err := parseMigrateArgs(os.Args[2:])
	if err != nil {
		return err
	}

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenantdb.NewPostgresRegistry(metaPool)

	var tenants []*tenant.Tenant

	if all {
		allTenants, listErr := registry.ListAll(ctx)
		if listErr != nil {
			return fmt.Errorf("list tenants: %w", listErr)
		}
		for _, t := range allTenants {
			if canMigrateTenant(t) {
				tenants = append(tenants, t)
			}
		}
	} else {
		t, getErr := registry.GetByID(ctx, targetID)
		if getErr != nil {
			return fmt.Errorf("get tenant %q: %w", targetID, getErr)
		}
		if !canMigrateTenant(t) {
			return fmt.Errorf("tenant %q cannot be migrated from status %s", targetID, t.Status)
		}
		tenants = []*tenant.Tenant{t}
	}

	dbUser := getEnvDefault("TENANT_DB_USER", "")
	dbPassword := getEnvDefault("TENANT_DB_PASSWORD", "")

	if dbUser == "" || dbPassword == "" {
		return fmt.Errorf("TENANT_DB_USER and TENANT_DB_PASSWORD are required")
	}

	if len(tenants) == 0 {
		fmt.Println("No active or suspended tenants to migrate")
		return nil
	}

	failures := make([]error, 0)
	for _, t := range tenants {
		fmt.Printf("Migrating %s (%s)...\n", t.Slug, t.DBName)

		dsn := tenantdb.DSN(t, dbUser, dbPassword)
		if migrateErr := runAllMigrations(dsn); migrateErr != nil {
			fmt.Printf("  ✗ Failed: %v\n", migrateErr)
			failures = append(failures, fmt.Errorf("tenant %s (%s): %w", t.Slug, t.ID, migrateErr))
			continue
		}

		if updateErr := registry.UpdateSchemaVersion(ctx, t.ID, version.ExpectedSchemaVersion); updateErr != nil {
			fmt.Printf("  ✗ Migrated, but failed to record schema version: %v\n", updateErr)
			failures = append(failures, fmt.Errorf("tenant %s (%s): record schema version: %w", t.Slug, t.ID, updateErr))
			continue
		}
		fmt.Printf("  ✓ Done (schema_version=%d)\n", version.ExpectedSchemaVersion)
	}

	return errors.Join(failures...)
}

func parseMigrateArgs(args []string) (targetID string, all bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--id":
			if i+1 >= len(args) {
				return "", false, fmt.Errorf("--id requires a tenant UUID")
			}
			targetID = args[i+1]
			i++
		case "--all":
			all = true
		default:
			return "", false, fmt.Errorf("unknown migrate option %q", args[i])
		}
	}

	if !all && targetID == "" {
		return "", false, fmt.Errorf("specify --id <tenant-uuid> or --all")
	}
	if all && targetID != "" {
		return "", false, fmt.Errorf("--id and --all are mutually exclusive")
	}
	return targetID, all, nil
}

func canMigrateTenant(t *tenant.Tenant) bool {
	return t.Status == tenant.StatusActive || t.Status == tenant.StatusSuspended
}

func suspendTenant(ctx context.Context) {
	if len(os.Args) < 3 {
		fmt.Println("Usage: tenant suspend <tenant-uuid>")
		os.Exit(1)
	}

	tenantID := os.Args[2]

	metaPool := getMetaPool(ctx)
	defer metaPool.Close()

	registry := tenantdb.NewPostgresRegistry(metaPool)
	t, err := registry.GetByID(ctx, tenantID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if t.Status != tenant.StatusActive {
		fmt.Printf("Error: tenant '%s' cannot be suspended from status %s\n", tenantID, t.Status)
		os.Exit(1)
	}
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

	registry := tenantdb.NewPostgresRegistry(metaPool)
	t, err := registry.GetByID(ctx, tenantID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if t.Status != tenant.StatusSuspended {
		fmt.Printf("Error: tenant '%s' cannot be activated from status %s\n", tenantID, t.Status)
		os.Exit(1)
	}
	if err := registry.UpdateStatusByID(ctx, tenantID, tenant.StatusActive); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Tenant '%s' activated\n", tenantID)
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
	value, err := secretfile.Get(key, fallback)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration for %s: %v\n", key, err)
		os.Exit(1)
	}
	return value
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

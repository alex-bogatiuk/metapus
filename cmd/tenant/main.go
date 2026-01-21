// Package main provides CLI for tenant management.
// Usage: tenant create --slug acme --name "ACME Corp"
//        tenant list
//        tenant migrate --all
//        tenant suspend <tenant-id>
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/tenant"
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
	case "migrate":
		migrateTenants(ctx)
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
	metaDSN := os.Getenv("META_DATABASE_URL")
	if metaDSN == "" {
		fmt.Println("Error: META_DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, metaDSN)
	if err != nil {
		fmt.Printf("Error connecting to meta database: %v\n", err)
		os.Exit(1)
	}

	return pool
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
	adminDSN := os.Getenv("POSTGRES_ADMIN_URL")
	if adminDSN == "" {
		// Try to construct from META_DATABASE_URL
		adminDSN = os.Getenv("META_DATABASE_URL")
		// Replace database name with 'postgres'
		adminDSN = strings.Replace(adminDSN, "/metapus_meta", "/postgres", 1)
	}

	if adminDSN != "" {
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
		tenantDSN := fmt.Sprintf("postgres://%s:%s@localhost:5432/%s?sslmode=disable",
			dbUser, dbPassword, dbName)

		cmd := exec.Command("goose", "-dir", "db/migrations", "postgres", tenantDSN, "up")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
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
		DBHost:      "localhost",
		DBPort:      5432,
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

	fmt.Printf("%-36s %-20s %-30s %-15s %-12s %-10s\n", "TENANT_ID", "SLUG", "NAME", "DATABASE", "PLAN", "STATUS")
	fmt.Println(strings.Repeat("-", 135))

	for _, t := range tenants {
		fmt.Printf("%-36s %-20s %-30s %-15s %-12s %-10s\n",
			truncate(t.ID, 36),
			truncate(t.Slug, 20),
			truncate(t.DisplayName, 30),
			truncate(t.DBName, 15),
			t.Plan,
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
		cmd := exec.Command("goose", "-dir", "db/migrations", "postgres", dsn, "up")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			fmt.Printf("  ✗ Failed: %v\n", err)
		} else {
			fmt.Printf("  ✓ Done\n")
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

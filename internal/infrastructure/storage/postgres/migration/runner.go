// Package migration provides reusable goose migration execution using the Go library API.
// Works in distroless containers (no goose CLI needed).
//
// Migration sources follow the numbering convention:
//   - Tenant (db/migrations/ for the platform and db/configuration/<name>/ for
//     each configuration): one sequence of versions 00001–09999, applied as a
//     single goose source; a version is never reused across directories
//   - Extensions (extensions/*/migrations/): versions 10000+ (e.g. 10001–10999, etc.)
//   - Additional (MIGRATION_EXTRA_DIRS): custom ranges
//
// All sources share a single goose_db_version table.
package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/pressly/goose/v3"

	// pgx stdlib adapter for database/sql — required by goose.
	_ "github.com/jackc/pgx/v5/stdlib"

	dbassets "metapus/db"
)

const (
	coreMigrationsDir = "db/migrations"
	metaMigrationsDir = "db/meta"
)

// Dirs returns ordered list of migration directories to apply.
// Tenant migrations (platform and configurations, see dbassets.TenantMigrations)
// always run first.
// Extension directories are auto-discovered from extensions/*/migrations/.
// Additional directories can be added via MIGRATION_EXTRA_DIRS (comma-separated).
//
// Ordering:
//
//  1. db/migrations/ + db/configuration/*/ (tenant — numbers 00001–09999)
//  2. extensions/*/migrations/ (auto-discovered — numbers 10000+)
//  3. MIGRATION_EXTRA_DIRS     (manual overrides)
func Dirs() []string {
	dirs := []string{coreMigrationsDir}

	// Auto-discover extension migration directories
	extEntries, err := os.ReadDir("extensions")
	if err == nil {
		for _, entry := range extEntries {
			if !entry.IsDir() {
				continue
			}
			migrDir := fmt.Sprintf("extensions/%s/migrations", entry.Name())
			if info, serr := os.Stat(migrDir); serr == nil && info.IsDir() {
				files, _ := os.ReadDir(migrDir)
				hasSQL := false
				for _, f := range files {
					if strings.HasSuffix(f.Name(), ".sql") {
						hasSQL = true
						break
					}
				}
				if hasSQL {
					dirs = append(dirs, migrDir)
				}
			}
		}
	}

	// Manual overrides via env var
	extra := os.Getenv("MIGRATION_EXTRA_DIRS")
	if extra != "" {
		for d := range strings.SplitSeq(extra, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// fsForDir returns the appropriate fs.FS for a migration directory.
// Tenant migrations come embedded from dbassets; extension directories use
// os.DirFS.
func fsForDir(dir string) (fs.FS, error) {
	if dir == coreMigrationsDir {
		return dbassets.TenantMigrations()
	}
	if dir == metaMigrationsDir {
		return fs.Sub(dbassets.Files, "meta")
	}
	// Extension or extra directories — use OS filesystem.
	return os.DirFS(dir), nil
}

// openDB creates a database/sql connection for goose.
// Uses pgx stdlib adapter under the hood.
func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return db, nil
}

// newProvider creates a goose.Provider for a specific migration directory.
// WithAllowMissing is required because core and extension directories share
// a single goose_db_version table — extensions may have higher version numbers
// than newly-added core migrations.
func newProvider(dir string, db *sql.DB) (*goose.Provider, error) {
	dirFS, err := fsForDir(dir)
	if err != nil {
		return nil, err
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, dirFS,
		goose.WithAllowOutofOrder(true),
	)
	if err != nil {
		return nil, fmt.Errorf("create goose provider for %s: %w", dir, err)
	}
	return provider, nil
}

// RunAll runs goose migrations from all configured directories in order.
// Core migrations run first, then each extension directory sequentially.
// Output is captured and returned as a combined string.
func RunAll(dsn string) (output string, err error) {
	return runDirectories(dsn, Dirs())
}

// RunMeta initializes and upgrades the control-plane metadata database.
func RunMeta(dsn string) (output string, err error) {
	return runDirectories(dsn, []string{metaMigrationsDir})
}

func runDirectories(dsn string, dirs []string) (output string, err error) {
	db, err := openDB(dsn)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	var combined strings.Builder
	ctx := context.Background()

	for _, dir := range dirs {
		provider, perr := newProvider(dir, db)
		if perr != nil {
			return combined.String(), fmt.Errorf("%s: %w", dir, perr)
		}

		results, uperr := provider.Up(ctx)
		for _, r := range results {
			if r.Error != nil {
				fmt.Fprintf(&combined, "[%s] migration %d FAILED: %v\n", dir, r.Source.Version, r.Error)
			} else {
				fmt.Fprintf(&combined, "[%s] migration %d applied (%.2fms)\n",
					dir, r.Source.Version, r.Duration.Seconds()*1000)
			}
		}

		if uperr != nil {
			return combined.String(), fmt.Errorf("%s: %w", dir, uperr)
		}
	}
	return combined.String(), nil
}

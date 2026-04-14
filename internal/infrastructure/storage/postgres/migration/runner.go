// Package migration provides reusable goose migration execution using the Go library API.
// Works in distroless containers (no goose CLI needed).
//
// Migration directories follow the numbering convention:
//   - Core (db/migrations/): versions 00001–09999
//   - Extensions (extensions/*/migrations/): versions 10000+ (vehicle=10001–10999, etc.)
//   - Additional (MIGRATION_EXTRA_DIRS): custom ranges
//
// All directories share a single goose_db_version table.
// Each goose.Provider only knows about its own SQL files, so DownTo() on one
// provider never touches migrations from another provider.
package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"

	"github.com/pressly/goose/v3"

	// pgx stdlib adapter for database/sql — required by goose.
	_ "github.com/jackc/pgx/v5/stdlib"
)

const coreMigrationsDir = "db/migrations"

// coreMigrationsFS holds an optional embedded FS for core migrations.
// Set by cmd packages via SetCoreMigrationsFS before RunAll is called.
// If nil, os.DirFS is used as fallback (works when binary runs from repo root).
var (
	coreMigrationsFS   fs.FS
	coreMigrationsMu   sync.Mutex
)

// SetCoreMigrationsFS sets the embedded filesystem for core migrations.
// Call this from cmd/server/main.go or cmd/tenant/main.go before any migration runs.
//
//	//go:embed db/migrations/*.sql
//	var coreMigrations embed.FS
//	migration.SetCoreMigrationsFS(coreMigrations)
func SetCoreMigrationsFS(fsys fs.FS) {
	coreMigrationsMu.Lock()
	defer coreMigrationsMu.Unlock()
	coreMigrationsFS = fsys
}

// Dirs returns ordered list of migration directories to apply.
// Core migrations from db/migrations/ always run first.
// Extension directories are auto-discovered from extensions/*/migrations/.
// Additional directories can be added via MIGRATION_EXTRA_DIRS (comma-separated).
//
// Ordering:
//
//	1. db/migrations/          (core — numbers 00001–09999)
//	2. extensions/*/migrations/ (auto-discovered — numbers 10000+)
//	3. MIGRATION_EXTRA_DIRS     (manual overrides)
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
		for _, d := range strings.Split(extra, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// fsForDir returns the appropriate fs.FS for a migration directory.
// Core migrations use injected embed.FS (via SetCoreMigrationsFS) if available,
// otherwise fall back to os.DirFS (for CLI usage outside containers).
// Extension directories always use os.DirFS.
func fsForDir(dir string) (fs.FS, error) {
	if dir == coreMigrationsDir {
		coreMigrationsMu.Lock()
		embeddedFS := coreMigrationsFS
		coreMigrationsMu.Unlock()

		if embeddedFS != nil {
			sub, err := fs.Sub(embeddedFS, coreMigrationsDir)
			if err != nil {
				return nil, fmt.Errorf("sub embed FS for %s: %w", dir, err)
			}
			return sub, nil
		}
		// Fallback: read from disk (CLI, dev mode)
		return os.DirFS(dir), nil
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
	db, err := openDB(dsn)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	var combined strings.Builder
	ctx := context.Background()

	for _, dir := range Dirs() {
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

// RunDownTo rolls back migrations to saved versions for each directory.
// Directories are processed in reverse order (extensions first, then core).
// Each provider only rolls back its own migrations (determined by its SQL files).
func RunDownTo(dsn string, targetVersions map[string]int64) (output string, err error) {
	db, err := openDB(dsn)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	var combined strings.Builder
	ctx := context.Background()

	// Process in reverse order: extensions first, then core.
	dirs := Dirs()
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]

		target, ok := targetVersions[dir]
		if !ok {
			// Directory not in snapshot — roll back to 0 (remove all its migrations).
			target = 0
		}

		provider, perr := newProvider(dir, db)
		if perr != nil {
			return combined.String(), fmt.Errorf("%s: %w", dir, perr)
		}

		results, dnerr := provider.DownTo(ctx, target)
		for _, r := range results {
			if r.Error != nil {
				fmt.Fprintf(&combined, "[%s] rollback %d FAILED: %v\n", dir, r.Source.Version, r.Error)
			} else {
				fmt.Fprintf(&combined, "[%s] rolled back %d (%.2fms)\n",
					dir, r.Source.Version, r.Duration.Seconds()*1000)
			}
		}

		if dnerr != nil {
			return combined.String(), fmt.Errorf("%s: rollback to %d: %w", dir, target, dnerr)
		}
	}
	return combined.String(), nil
}

// GetCurrentVersions returns the latest applied goose version for each migration directory.
// Used to snapshot the state before running migrations, enabling rollback.
func GetCurrentVersions(dsn string) (map[string]int64, error) {
	db, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	versions := make(map[string]int64)

	for _, dir := range Dirs() {
		provider, perr := newProvider(dir, db)
		if perr != nil {
			return nil, fmt.Errorf("%s: %w", dir, perr)
		}

		ver, verr := provider.GetDBVersion(ctx)
		if verr != nil {
			return nil, fmt.Errorf("%s: get version: %w", dir, verr)
		}
		versions[dir] = ver
	}
	return versions, nil
}

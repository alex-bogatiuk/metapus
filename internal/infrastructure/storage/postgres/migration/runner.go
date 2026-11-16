// Package migration provides reusable goose migration execution using the Go library API.
// Works in distroless containers (no goose CLI needed).
//
// Migration sources follow the numbering convention:
//   - Tenant (db/migrations/ for the platform and db/configuration/<name>/ for
//     each configuration and each extension module): one goose source embedded
//     in the binary. The platform and configurations use versions 00001–09999,
//     extensions 10000+; a version is never reused across directories
//   - Additional (MIGRATION_EXTRA_DIRS): custom ranges read from disk
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
// Tenant migrations (platform, configurations and extensions, see
// dbassets.TenantMigrations) come embedded and always run first. Additional
// directories can be added via MIGRATION_EXTRA_DIRS (comma-separated).
func Dirs() []string {
	dirs := []string{coreMigrationsDir}
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
// Tenant migrations come embedded from dbassets; MIGRATION_EXTRA_DIRS use
// os.DirFS.
func fsForDir(dir string) (fs.FS, error) {
	if dir == coreMigrationsDir {
		return dbassets.TenantMigrations()
	}
	if dir == metaMigrationsDir {
		return fs.Sub(dbassets.Files, "meta")
	}
	// Extra directories — use OS filesystem.
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
// WithAllowOutofOrder is required because every source shares a single
// goose_db_version table: an extension (10000+) may already be applied when a
// new platform or configuration migration arrives with a lower version.
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
// Tenant migrations run first, then each MIGRATION_EXTRA_DIRS entry. With
// WithPermissions it then syncs the registry's permissions (SyncPermissions).
// Output is captured and returned as a combined string.
func RunAll(dsn string, opts ...Option) (output string, err error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return runDirectories(dsn, Dirs(), o)
}

// RunMeta initializes and upgrades the control-plane metadata database.
func RunMeta(dsn string) (output string, err error) {
	return runDirectories(dsn, []string{metaMigrationsDir}, options{})
}

func runDirectories(dsn string, dirs []string, o options) (output string, err error) {
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
	if len(o.permissions) > 0 {
		synced, serr := SyncPermissions(ctx, db, o.permissions)
		if serr != nil {
			return combined.String(), serr
		}
		fmt.Fprintf(&combined, "[permissions] %d created and granted to %s, %d regrouped\n",
			len(synced.Created), AdminRoleCode, len(synced.Updated))
	}
	return combined.String(), nil
}

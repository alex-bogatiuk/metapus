// Package migration provides reusable goose migration execution using the Go library API.
// Works in distroless containers (no goose CLI needed).
//
// A tenant database gets three migration sources, all embedded in the binary
// (see dbassets), each with a goose history of its own:
//   - the platform's (db/migrations), in goose_db_version_platform;
//   - its configuration's (db/configuration/<name>), in
//     goose_db_version_<name>; a tenant database holds one configuration's
//     migrations for life;
//   - the client extension modules' (db/extensions/<module>), versions
//     10000+, in goose_db_version_extensions.
//
// A database migrated before the histories were split keeps them all in
// goose_db_version; Run moves them to their tables once (adoptSharedHistory).
package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"

	// pgx stdlib adapter for database/sql — required by goose.
	_ "github.com/jackc/pgx/v5/stdlib"

	dbassets "github.com/alex-bogatiuk/metapus/db"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
)

const (
	// PlatformVersionTable is the goose history of the platform's migrations.
	PlatformVersionTable = "goose_db_version_platform"
	// ExtensionsVersionTable is the goose history of the extension modules'
	// migrations.
	ExtensionsVersionTable = "goose_db_version_extensions"

	// sharedVersionTable is the one goose history every tenant migration
	// shared before the split; meta databases still use it.
	sharedVersionTable = "goose_db_version"
	// sharedPlatformVersion is the platform's last migration while the
	// history was shared: every version above it there is a configuration's
	// or, from dbassets.FirstExtensionMigration, an extension module's.
	sharedPlatformVersion = 5
)

// configurationName matches configuration.ValidName.
var configurationName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ConfigurationVersionTable is the goose history of configuration name's
// migrations in a tenant database.
func ConfigurationVersionTable(name string) string { return "goose_db_version_" + name }

// WithConfiguration makes Run apply configuration name's tenant migrations
// after the platform's; source is nil when the configuration ships none. A
// database that holds another configuration's migrations is refused. Without
// it Run applies the platform's and the extensions' migrations to a database
// of any configuration.
func WithConfiguration(name string, source fs.FS) Option {
	return func(o *options) {
		o.configuration = name
		o.configurationSource = source
	}
}

// Result is what Run applied.
type Result struct {
	// Output lists every migration applied and every step taken.
	Output string
	// Schema is the database's migration state after the run.
	Schema tenant.Schema
}

// RunAll is Run that returns only its output.
func RunAll(dsn string, opts ...Option) (output string, err error) {
	result, err := Run(dsn, opts...)
	return result.Output, err
}

// Run migrates a tenant database: the platform's migrations, the
// configuration's (WithConfiguration), then the extension modules'. With
// WithPermissions it then syncs the registry's permissions
// (SyncPermissions), with WithAccumulationRegisters it checks the declarative
// registers' tables (CheckAccumulationRegisters), and with WithCatalogCodes
// it numbers catalog items stored without a code (BackfillCatalogCodes).
func Run(dsn string, opts ...Option) (result Result, err error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	result.Schema.Configuration = o.configuration
	if o.configuration != "" && (!configurationName.MatchString(o.configuration) || o.configuration == "platform" || o.configuration == "extensions") {
		return result, fmt.Errorf("invalid configuration name %q", o.configuration)
	}
	db, err := openDB(dsn)
	if err != nil {
		return result, err
	}
	defer func() { _ = db.Close() }()

	var out strings.Builder
	defer func() { result.Output = out.String() }()
	ctx := context.Background()

	if err := adoptSharedHistory(ctx, db, o, &out); err != nil {
		return result, err
	}
	if o.configuration != "" {
		if err := refuseOtherConfigurations(ctx, db, o.configuration); err != nil {
			return result, err
		}
		// A configuration without migrations still marks the database as
		// its own with an empty history.
		if o.configurationSource == nil {
			if err := ensureHistory(ctx, db, ConfigurationVersionTable(o.configuration)); err != nil {
				return result, err
			}
		}
	}
	sources, err := tenantSources(o)
	if err != nil {
		return result, err
	}
	for _, source := range sources {
		provider, err := source.provider(db)
		if err != nil {
			return result, err
		}
		results, uperr := provider.Up(ctx)
		for _, r := range results {
			if r.Error != nil {
				fmt.Fprintf(&out, "[%s] migration %d FAILED: %v\n", source.name, r.Source.Version, r.Error)
			} else {
				fmt.Fprintf(&out, "[%s] migration %d applied (%.2fms)\n", source.name, r.Source.Version, r.Duration.Seconds()*1000)
			}
		}
		if uperr != nil {
			return result, fmt.Errorf("%s migrations: %w", source.name, uperr)
		}
		version, err := provider.GetDBVersion(ctx)
		if err != nil {
			return result, fmt.Errorf("%s schema version: %w", source.name, err)
		}
		switch source.table {
		case PlatformVersionTable:
			result.Schema.Version = int(version)
		case ConfigurationVersionTable(o.configuration):
			result.Schema.ConfigurationVersion = int(version)
		}
	}
	if len(o.permissions) > 0 {
		synced, serr := SyncPermissions(ctx, db, o.permissions)
		if serr != nil {
			return result, serr
		}
		fmt.Fprintf(&out, "[permissions] %d created and granted to %s, %d regrouped\n",
			len(synced.Created), AdminRoleCode, len(synced.Updated))
	}
	if len(o.registers) > 0 {
		if err := CheckAccumulationRegisters(ctx, db, o.registers); err != nil {
			return result, err
		}
		fmt.Fprintf(&out, "[registers] %d accumulation registers match their tables\n", len(o.registers))
	}
	if len(o.catalogs) > 0 {
		assigned, berr := BackfillCatalogCodes(ctx, db, o.catalogs)
		if berr != nil {
			return result, berr
		}
		for _, def := range o.catalogs {
			if n := assigned[def.Key]; n > 0 {
				fmt.Fprintf(&out, "[catalog codes] %s: %d assigned\n", def.Key, n)
				delete(assigned, def.Key)
			}
		}
	}
	return result, nil
}

// RunMeta initializes and upgrades the control-plane metadata database.
func RunMeta(dsn string) (output string, err error) {
	db, err := openDB(dsn)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()
	source, err := fs.Sub(dbassets.Files, "meta")
	if err != nil {
		return "", err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, source)
	if err != nil {
		return "", fmt.Errorf("create goose provider for meta: %w", err)
	}
	var out strings.Builder
	results, err := provider.Up(context.Background())
	for _, r := range results {
		fmt.Fprintf(&out, "[meta] migration %d applied (%.2fms)\n", r.Source.Version, r.Duration.Seconds()*1000)
	}
	if err != nil {
		return out.String(), fmt.Errorf("meta migrations: %w", err)
	}
	return out.String(), nil
}

// tenantSource is one migration source of a tenant database with its goose
// history.
type tenantSource struct {
	name  string
	table string
	files fs.FS
	// outOfOrder lets a lower version apply after a higher one: extension
	// modules land in any order.
	outOfOrder bool
}

func (s tenantSource) provider(db *sql.DB) (*goose.Provider, error) {
	provider, err := goose.NewProvider(goose.DialectPostgres, db, s.files,
		goose.WithTableName(s.table), goose.WithAllowOutofOrder(s.outOfOrder))
	if err != nil {
		return nil, fmt.Errorf("create goose provider for %s: %w", s.name, err)
	}
	return provider, nil
}

// tenantSources lists a tenant database's migration sources in the order
// they apply: the platform's, the configuration's, the extension modules'.
func tenantSources(o options) ([]tenantSource, error) {
	platform, err := dbassets.PlatformMigrations()
	if err != nil {
		return nil, err
	}
	sources := []tenantSource{{name: "platform", table: PlatformVersionTable, files: platform}}
	if o.configurationSource != nil {
		sources = append(sources, tenantSource{name: o.configuration, table: ConfigurationVersionTable(o.configuration), files: o.configurationSource})
	}
	extensions, err := dbassets.ExtensionMigrations()
	if err != nil {
		return nil, err
	}
	if names, _ := fs.Glob(extensions, "*.sql"); len(names) > 0 {
		sources = append(sources, tenantSource{name: "extensions", table: ExtensionsVersionTable, files: extensions, outOfOrder: true})
	}
	return sources, nil
}

// refuseOtherConfigurations fails when the database keeps the goose history
// of a configuration other than configuration.
func refuseOtherConfigurations(ctx context.Context, db *sql.DB, configuration string) error {
	rows, err := db.QueryContext(ctx, `
		SELECT tablename FROM pg_tables
		WHERE schemaname = current_schema() AND tablename LIKE 'goose\_db\_version\_%'
		ORDER BY tablename`)
	if err != nil {
		return fmt.Errorf("list goose histories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return err
		}
		if table == PlatformVersionTable || table == ExtensionsVersionTable || table == ConfigurationVersionTable(configuration) {
			continue
		}
		return fmt.Errorf("%w: the database holds the migrations of configuration %q, not %q",
			tenant.ErrConfigurationMismatch, strings.TrimPrefix(table, "goose_db_version_"), configuration)
	}
	return rows.Err()
}

// sharedRow is one row of the shared goose history.
type sharedRow struct {
	version int64
	applied bool
	tstamp  sql.NullTime
}

// adoptSharedHistory moves the shared goose history of a database migrated
// before the split to the platform's, the configuration's and the
// extensions' tables, then drops it. Versions up to sharedPlatformVersion
// are the platform's, versions from dbassets.FirstExtensionMigration the
// extensions', the rest the configuration's: each must be a migration of the
// configuration being applied.
func adoptSharedHistory(ctx context.Context, db *sql.DB, o options, out *strings.Builder) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'))`); err != nil {
		return err
	}
	var shared bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('`+sharedVersionTable+`') IS NOT NULL`).Scan(&shared); err != nil {
		return err
	}
	if !shared {
		return tx.Rollback()
	}
	rows, err := tx.QueryContext(ctx, `SELECT version_id, is_applied, tstamp FROM `+sharedVersionTable+` ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read the shared goose history: %w", err)
	}
	var platform, configuration, extensions []sharedRow
	for rows.Next() {
		var row sharedRow
		if err = rows.Scan(&row.version, &row.applied, &row.tstamp); err != nil {
			_ = rows.Close()
			return err
		}
		switch {
		case row.version <= sharedPlatformVersion:
			platform = append(platform, row)
		case row.version >= dbassets.FirstExtensionMigration:
			extensions = append(extensions, row)
		default:
			configuration = append(configuration, row)
		}
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if len(configuration) > 0 {
		if err = checkConfigurationHistory(configuration, o); err != nil {
			return err
		}
	}
	for _, history := range []struct {
		table string
		rows  []sharedRow
	}{
		{PlatformVersionTable, platform},
		{ConfigurationVersionTable(o.configuration), configuration},
		{ExtensionsVersionTable, extensions},
	} {
		if len(history.rows) == 0 {
			continue
		}
		if err = copyHistory(ctx, tx, history.table, history.rows); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DROP TABLE `+sharedVersionTable); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	fmt.Fprintf(out, "[history] moved the shared goose history: %d platform, %d %s, %d extension versions\n",
		len(platform), len(configuration), o.configuration, len(extensions))
	return nil
}

// checkConfigurationHistory fails unless every configuration version of the
// shared history is a migration of the configuration being applied.
func checkConfigurationHistory(rows []sharedRow, o options) error {
	var versions []int64
	for _, row := range rows {
		versions = append(versions, row.version)
	}
	if o.configurationSource == nil {
		return fmt.Errorf("%w: the database holds configuration migrations %v; migrate it with its configuration", tenant.ErrConfigurationMismatch, versions)
	}
	names, err := fs.Glob(o.configurationSource, "*.sql")
	if err != nil {
		return err
	}
	known := map[int64]bool{}
	for _, name := range names {
		var version int64
		if _, err := fmt.Sscanf(name, "%05d_", &version); err == nil {
			known[version] = true
		}
	}
	if slices.ContainsFunc(versions, func(v int64) bool { return !known[v] }) {
		return fmt.Errorf("%w: the database holds configuration migrations %v that are not all %q's", tenant.ErrConfigurationMismatch, versions, o.configuration)
	}
	return nil
}

// copyHistory creates goose history table with rows; it must not exist yet.
func copyHistory(ctx context.Context, tx *sql.Tx, table string, rows []sharedRow) error {
	store, err := database.NewStore(database.DialectPostgres, table)
	if err != nil {
		return err
	}
	if err := store.CreateVersionTable(ctx, tx); err != nil {
		return fmt.Errorf("create %s: %w", table, err)
	}
	if rows[0].version != 0 {
		rows = append([]sharedRow{{version: 0, applied: true}}, rows...)
	}
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (version_id, is_applied, tstamp) VALUES ($1, $2, COALESCE($3, now()))`,
			row.version, row.applied, row.tstamp); err != nil {
			return fmt.Errorf("copy history to %s: %w", table, err)
		}
	}
	return nil
}

// ensureHistory creates goose history table, holding only version 0, unless
// it exists.
func ensureHistory(ctx context.Context, db *sql.DB, table string) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'))`); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Rollback()
	}
	if err = copyHistory(ctx, tx, table, []sharedRow{{version: 0, applied: true}}); err != nil {
		return err
	}
	return tx.Commit()
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

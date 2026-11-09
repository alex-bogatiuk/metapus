//go:build integration

package migration

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	dbassets "metapus/db"
	"metapus/internal/core/id"
)

func TestWorkerReliabilityLegacyUpgradeMatchesFreshSchema(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_ADMIN_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_ADMIN_DSN is required")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	defer func() { _ = admin.Close(context.Background()) }()
	root, err := url.Parse(dsn)
	require.NoError(t, err)
	current, err := fs.Sub(dbassets.Files, "migrations")
	require.NoError(t, err)
	legacy := fstest.MapFS{}
	require.NoError(t, fs.WalkDir(current, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := fs.ReadFile(current, path)
		if err != nil {
			return err
		}
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		if strings.HasPrefix(path, "00002_") || strings.HasPrefix(path, "00044_") {
			text = regexp.MustCompile(`(?m)^\s*(claim_token|claimed_at|lease_until)\s+[^\n]*\n`).ReplaceAllString(text, "")
		}
		if strings.HasPrefix(path, "00002_") {
			text = strings.Replace(text, "ON sys_outbox (lease_until)", "ON sys_outbox (created_at)", 1)
		}
		if strings.HasPrefix(path, "00044_") {
			text = regexp.MustCompile(`(?m)^CREATE (UNIQUE )?INDEX (uq_webhook_delivery_jobs_processing_merchant|idx_webhook_delivery_jobs_expired|idx_webhook_delivery_jobs_last_attempt)\n[^;]+;\n`).ReplaceAllString(text, "")
		}
		if strings.HasPrefix(path, "00013_") {
			before, _, ok := strings.Cut(text, "-- Persisted cursors")
			if !ok {
				return fs.ErrInvalid
			}
			_, down, ok := strings.Cut(text, "-- +goose Down")
			if !ok {
				return fs.ErrInvalid
			}
			text = before + "-- +goose Down" + down
		}
		legacy[path] = &fstest.MapFile{Data: []byte(text), Mode: 0644}
		return nil
	}))
	upgrade, err := dbassets.Files.ReadFile("upgrades/worker_reliability.sql")
	require.NoError(t, err)
	var schemas []string
	for i, source := range []fs.FS{current, legacy} {
		name := "worker_reliability_" + id.Compact(id.New())
		_, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
		require.NoError(t, err)
		defer func() {
			_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		}()
		address := *root
		address.Path = "/" + name
		db, err := sql.Open("pgx", address.String())
		require.NoError(t, err)
		defer func() { _ = db.Close() }()
		provider, err := goose.NewProvider(goose.DialectPostgres, db, source)
		require.NoError(t, err)
		_, err = provider.Up(t.Context())
		require.NoError(t, err)
		if i == 1 {
			// Legacy replicas could leave multiple HTTP claims for the same merchant.
			_, err = db.ExecContext(t.Context(), `
 INSERT INTO cat_merchants(id,code,name) VALUES('00000000-0000-0000-0000-000000000001','upgrade','upgrade');
 INSERT INTO sys_merchant_events(id,event_id,merchant_id,aggregate_type,aggregate_id,event_type,api_version,resource_version,causation_key,payload,created_at)
 VALUES('00000000-0000-0000-0000-000000000002','evt_upgrade','00000000-0000-0000-0000-000000000001','test','00000000-0000-0000-0000-000000000002','test','test',1,'test','{}',now());
 INSERT INTO sys_webhook_delivery_jobs(event_id,merchant_id,snapshot_url,secret_ref,secret_version,deadline_at,status)
 SELECT 'evt_upgrade','00000000-0000-0000-0000-000000000001','https://example.com','test',1,now()+INTERVAL '1 day','processing' FROM generate_series(1,2);
 INSERT INTO sys_outbox(id,aggregate_type,aggregate_id,event_type,payload,status)
 VALUES(gen_random_uuid_v7(),'test','00000000-0000-0000-0000-000000000002','test','{}','processing');`)
			require.NoError(t, err)
			var before int64
			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT MAX(version_id) FROM goose_db_version`).Scan(&before))
			for range 2 {
				_, err = db.ExecContext(t.Context(), string(upgrade))
				require.NoError(t, err)
			}
			var claims int
			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT (SELECT COUNT(*) FROM sys_outbox WHERE status='processing')+(SELECT COUNT(*) FROM sys_webhook_delivery_jobs WHERE status='processing')`).Scan(&claims))
			require.Zero(t, claims)
			var after int64
			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT MAX(version_id) FROM goose_db_version`).Scan(&after))
			require.Equal(t, before, after)
		}
		var schema string
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT jsonb_build_object(
 'columns',(SELECT jsonb_agg(jsonb_build_array(table_name,column_name,data_type,is_nullable,column_default) ORDER BY table_name,column_name) FROM information_schema.columns WHERE table_schema='public'),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(conrelid::regclass::text,conname,pg_get_constraintdef(oid)) ORDER BY conrelid::regclass::text,conname) FROM pg_constraint WHERE connamespace='public'::regnamespace),
 'indexes',(SELECT jsonb_agg(jsonb_build_array(tablename,indexname,indexdef) ORDER BY tablename,indexname) FROM pg_indexes WHERE schemaname='public'),
 'triggers',(SELECT jsonb_agg(jsonb_build_array(tgrelid::regclass::text,tgname,pg_get_triggerdef(oid)) ORDER BY tgrelid::regclass::text,tgname) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN (SELECT oid FROM pg_class WHERE relnamespace='public'::regnamespace)))::text`).Scan(&schema))
		schemas = append(schemas, schema)
	}
	require.JSONEq(t, schemas[0], schemas[1])
}

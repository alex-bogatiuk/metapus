//go:build integration

package migration

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFreshSchemaAuthNamesUUIDDefaultsAndTenantBindingExceptions(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_MIGRATION_DSN")
	require.NotEmpty(t, dsn, "explicit disposable migration database required")
	output, err := RunAll(dsn)
	require.NoError(t, err, output)
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	for _, old := range []string{"users", "roles", "permissions", "role_permissions", "user_roles", "refresh_tokens", "security_profiles", "security_profile_dimensions", "security_profile_field_policies", "user_security_profiles", "security_policy_rules", "user_mfa_totp", "user_preferences"} {
		name := "auth_" + old
		if old == "user_preferences" {
			name = "sys_user_preferences"
		}
		var absent, present bool
		require.NoError(t, db.QueryRow(`SELECT to_regclass($1) IS NULL, to_regclass($2) IS NOT NULL`, old, name).Scan(&absent, &present))
		require.True(t, absent, old)
		require.True(t, present, name)
	}
	rows, err := db.Query(`SELECT table_name, column_name, column_default FROM information_schema.columns WHERE table_schema='public' AND udt_name='uuid' AND column_default IS NOT NULL`)
	require.NoError(t, err)
	count := 0
	for rows.Next() {
		var table, column, expression string
		require.NoError(t, rows.Scan(&table, &column, &expression))
		require.Equal(t, "gen_random_uuid_v7()", expression, table+"."+column)
		count++
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Greater(t, count, 20)
	var generated string
	require.NoError(t, db.QueryRow(`SELECT gen_random_uuid_v7()::text`).Scan(&generated))
	require.Equal(t, uuid.Version(7), uuid.MustParse(generated).Version())

	rows, err = db.Query(`SELECT table_name, is_nullable FROM information_schema.columns WHERE table_schema='public' AND column_name='tenant_id' ORDER BY table_name`)
	require.NoError(t, err)
	var bound []string
	for rows.Next() {
		var table, nullable string
		require.NoError(t, rows.Scan(&table, &nullable))
		require.Equal(t, "NO", nullable, table)
		bound = append(bound, table)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	// Tenant is part of these cryptographic authorization identities, not an RLS column.
	require.Equal(t, []string{"auth_step_up_confirmations", "reg_crypto_signing_authorizations"}, bound)
	var immutableSigningIdentity string
	require.NoError(t, db.QueryRow(`SELECT pg_get_functiondef(p.oid) FROM pg_proc p WHERE p.proname='fn_crypto_signing_auth_identity_immutable'`).Scan(&immutableSigningIdentity))
	require.Contains(t, strings.ToLower(immutableSigningIdentity), "tenant_id")
}

// Every document table holds a number at most once, so a retried write or a
// stale copy cannot store a number another document already has. Documents
// stored before they are numbered keep an empty number and may share it.
func TestFreshSchemaEveryDocumentNumberIsUnique(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_MIGRATION_DSN")
	require.NotEmpty(t, dsn, "explicit disposable migration database required")
	output, err := RunAll(dsn)
	require.NoError(t, err, output)
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	rows, err := db.Query(`
		SELECT c.table_name,
		       EXISTS (
		           SELECT 1 FROM pg_index i
		           JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
		           WHERE i.indrelid = to_regclass(c.table_name) AND i.indisunique
		             AND i.indnkeyatts = 1 AND a.attname = 'number')
		FROM information_schema.columns c
		WHERE c.table_schema = 'public' AND c.column_name = 'number'
		  AND c.table_name LIKE 'doc\_%' AND c.table_name NOT LIKE '%\_\_%'
		ORDER BY c.table_name`)
	require.NoError(t, err)
	var checked []string
	for rows.Next() {
		var table string
		var unique bool
		require.NoError(t, rows.Scan(&table, &unique))
		require.True(t, unique, "%s.number has no unique index", table)
		checked = append(checked, table)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Contains(t, checked, "doc_crypto_withdrawals")
	require.Contains(t, checked, "doc_crypto_native_fundings")
	require.GreaterOrEqual(t, len(checked), 13)
}

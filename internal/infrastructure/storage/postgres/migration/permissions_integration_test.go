//go:build integration

package migration

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
)

func migratedPermissionDB(t *testing.T, opts ...Option) *sql.DB {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_MIGRATION_DSN")
	require.NotEmpty(t, dsn, "explicit disposable migration database required")
	output, err := RunAll(dsn, append([]Option{withCrypto(t)}, opts...)...)
	require.NoError(t, err, output)
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func adminHas(t *testing.T, db *sql.DB, code string) bool {
	t.Helper()
	var granted bool
	require.NoError(t, db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM auth_role_permissions rp
		JOIN auth_roles r ON r.id = rp.role_id
		JOIN auth_permissions p ON p.id = rp.permission_id
		WHERE r.code = 'admin' AND p.code = $1)`, code).Scan(&granted))
	return granted
}

func TestRunAllSyncsRegistryPermissionsAndGrantsOnlyNewOnesToAdmin(t *testing.T) {
	key := "sync_probe_" + strings.ToLower(id.Compact(id.New()))[:12]
	read := auth.Permission{Code: "catalog:" + key + ":read", Name: "Пробы: чтение", Resource: key, Action: "read"}
	create := auth.Permission{Code: "catalog:" + key + ":create", Name: "Пробы: создание", Resource: key, Action: "create"}
	db := migratedPermissionDB(t, WithPermissions([]auth.Permission{read, create}))
	ctx := context.Background()

	require.True(t, adminHas(t, db, read.Code))
	require.True(t, adminHas(t, db, create.Code))

	// A second run creates nothing and respects a revocation from admin.
	_, err := db.Exec(`DELETE FROM auth_role_permissions WHERE permission_id = (SELECT id FROM auth_permissions WHERE code = $1)`, create.Code)
	require.NoError(t, err)
	synced, err := SyncPermissions(ctx, db, []auth.Permission{read, create})
	require.NoError(t, err)
	require.Empty(t, synced.Created)
	require.Empty(t, synced.Updated)
	require.False(t, adminHas(t, db, create.Code), "a revoked permission must not be granted again")

	// An existing row keeps its curated name; only its grouping is corrected.
	_, err = db.Exec(`UPDATE auth_permissions SET name = 'Курируемое имя', resource = 'catalog:' || resource WHERE code = $1`, read.Code)
	require.NoError(t, err)
	synced, err = SyncPermissions(ctx, db, []auth.Permission{read})
	require.NoError(t, err)
	require.Equal(t, []string{read.Code}, synced.Updated)
	var name, resource string
	require.NoError(t, db.QueryRow(`SELECT name, resource FROM auth_permissions WHERE code = $1`, read.Code).Scan(&name, &resource))
	require.Equal(t, "Курируемое имя", name)
	require.Equal(t, key, resource)

	_, err = SyncPermissions(ctx, db, []auth.Permission{{Code: "catalog:" + key + ":bad"}})
	require.Error(t, err)
}

func TestPlatformPermissionCodesMatchTheRoutes(t *testing.T) {
	db := migratedPermissionDB(t)
	var dotted int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM auth_permissions
		WHERE code IN ('counterparty.read', 'currency.create', 'organization.update', 'contract.delete', 'report_documents.read')`).Scan(&dotted))
	require.Zero(t, dotted, "dotted platform codes match no route")

	for role, code := range map[string]string{
		"accountant": "catalog:counterparty:read",
		"manager":    "catalog:contract:update",
		"user":       "catalog:currency:read",
	} {
		var granted bool
		require.NoError(t, db.QueryRow(`SELECT EXISTS(
			SELECT 1 FROM auth_role_permissions rp
			JOIN auth_roles r ON r.id = rp.role_id
			JOIN auth_permissions p ON p.id = rp.permission_id
			WHERE r.code = $1 AND p.code = $2)`, role, code).Scan(&granted))
		require.Truef(t, granted, "%s keeps its seeded grant under the canonical code %s", role, code)
	}
	var journal string
	require.NoError(t, db.QueryRow(`SELECT resource FROM auth_permissions WHERE code = 'report:document-journal:read'`).Scan(&journal))
	require.Equal(t, "report_document_journal", journal)
}

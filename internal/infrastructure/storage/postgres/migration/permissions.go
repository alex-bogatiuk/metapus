package migration

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/accumulation"
	"github.com/alex-bogatiuk/metapus/internal/domain/registers/information"
)

// AdminRoleCode is the system role that receives every newly created permission.
const AdminRoleCode = "admin"

// Option configures Run.
type Option func(*options)

type options struct {
	configuration       string
	configurationSource fs.FS
	permissions         []auth.Permission
	catalogs            []entity.Definition
	registers           []*accumulation.Schema
	information         []*information.Schema
}

// WithPermissions makes RunAll sync the given permissions into the tenant
// database after its migrations (see SyncPermissions). Pass the registry's
// permissions, v1.FactoryRegistry.Permissions, from the composition root.
func WithPermissions(permissions []auth.Permission) Option {
	return func(o *options) { o.permissions = append(o.permissions, permissions...) }
}

// PermissionSync reports what SyncPermissions changed.
type PermissionSync struct {
	// Created are the codes that did not exist and were granted to admin.
	Created []string
	// Updated are existing codes whose resource or action was corrected.
	Updated []string
}

// SyncPermissions makes every given permission exist in auth_permissions.
//
// A missing code is created and granted to the admin role. An existing code
// keeps its name, description and role assignments: only its resource and
// action are brought in line with the code, so the role editor groups it with
// its entity. Nothing is deleted: a code that left the registry stays until a
// migration removes it. A permission revoked from admin is not granted again,
// because only codes created by this call are granted.
func SyncPermissions(ctx context.Context, db *sql.DB, permissions []auth.Permission) (PermissionSync, error) {
	var result PermissionSync
	if len(permissions) == 0 {
		return result, nil
	}
	codes := make([]string, 0, len(permissions))
	names := make([]string, 0, len(permissions))
	resources := make([]string, 0, len(permissions))
	actions := make([]string, 0, len(permissions))
	seen := make(map[string]bool, len(permissions))
	for _, p := range permissions {
		if p.Code == "" || p.Resource == "" || p.Action == "" || p.Name == "" {
			return result, fmt.Errorf("permission %q: code, name, resource and action are required", p.Code)
		}
		if seen[p.Code] {
			continue
		}
		seen[p.Code] = true
		codes = append(codes, p.Code)
		names = append(names, p.Name)
		resources = append(resources, p.Resource)
		actions = append(actions, p.Action)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	// The same lock as the migrations: concurrent runs sync one at a time.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('metapus_migrations'))`); err != nil {
		return result, err
	}

	rows, err := tx.QueryContext(ctx, `
		INSERT INTO auth_permissions (code, name, resource, action)
		SELECT * FROM unnest($1::text[], $2::text[], $3::text[], $4::text[])
		ON CONFLICT (code) DO UPDATE
			SET resource = EXCLUDED.resource, action = EXCLUDED.action
			WHERE auth_permissions.resource IS DISTINCT FROM EXCLUDED.resource
			   OR auth_permissions.action IS DISTINCT FROM EXCLUDED.action
		RETURNING id::text, code, (xmax = 0) AS created`,
		codes, names, resources, actions)
	if err != nil {
		return result, fmt.Errorf("sync permissions: %w", err)
	}
	var created []string
	for rows.Next() {
		var permissionID, code string
		var isNew bool
		if err := rows.Scan(&permissionID, &code, &isNew); err != nil {
			_ = rows.Close()
			return result, err
		}
		if isNew {
			created = append(created, permissionID)
			result.Created = append(result.Created, code)
		} else {
			result.Updated = append(result.Updated, code)
		}
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	if len(created) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auth_role_permissions (role_id, permission_id)
			SELECT r.id, p.id::uuid
			FROM auth_roles r CROSS JOIN unnest($1::text[]) AS p(id)
			WHERE r.code = $2
			ON CONFLICT DO NOTHING`, created, AdminRoleCode); err != nil {
			return result, fmt.Errorf("grant permissions to %s: %w", AdminRoleCode, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// Package auth_repo provides PostgreSQL implementations for auth repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context.
package auth_repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/storage/postgres"
)

// RoleRepo implements auth.RoleRepository.
// In Database-per-Tenant, TxManager is obtained from context.
type RoleRepo struct{}

// NewRoleRepo creates a new role repository.
func NewRoleRepo() *RoleRepo {
	return &RoleRepo{}
}

// getTxManager retrieves TxManager from context.
func (r *RoleRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// Create creates a new role.
func (r *RoleRepo) Create(ctx context.Context, role *auth.Role) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO roles (id, code, name, description, is_system, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := q.Exec(ctx, query,
		role.ID, role.Code, role.Name,
		role.Description, role.IsSystem, role.CreatedAt, role.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert role: %w", err)
	}

	return nil
}

// GetByID retrieves role by ID.
func (r *RoleRepo) GetByID(ctx context.Context, roleID id.ID) (*auth.Role, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, code, name, description, is_system, created_at, updated_at
		FROM roles WHERE id = $1
	`

	var role auth.Role
	err := q.QueryRow(ctx, query, roleID).Scan(
		&role.ID, &role.Code, &role.Name,
		&role.Description, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("role", roleID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("query role: %w", err)
	}

	return &role, nil
}

// GetByCode retrieves role by code (within tenant database).
func (r *RoleRepo) GetByCode(ctx context.Context, code string) (*auth.Role, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, code, name, description, is_system, created_at, updated_at
		FROM roles WHERE code = $1
	`

	var role auth.Role
	err := q.QueryRow(ctx, query, code).Scan(
		&role.ID, &role.Code, &role.Name,
		&role.Description, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("role", code)
	}
	if err != nil {
		return nil, fmt.Errorf("query role: %w", err)
	}

	return &role, nil
}

// Update updates role data.
func (r *RoleRepo) Update(ctx context.Context, role *auth.Role) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		UPDATE roles SET name = $2, description = $3, updated_at = now()
		WHERE id = $1
	`

	_, err := q.Exec(ctx, query, role.ID, role.Name, role.Description)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}

	return nil
}

// Delete deletes a role (only non-system roles).
func (r *RoleRepo) Delete(ctx context.Context, roleID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `DELETE FROM roles WHERE id = $1 AND is_system = false`
	result, err := q.Exec(ctx, query, roleID)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewBusinessRule("CANNOT_DELETE_SYSTEM_ROLE", "Cannot delete system role")
	}

	return nil
}

// List retrieves roles (within tenant database).
func (r *RoleRepo) List(ctx context.Context) ([]auth.Role, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, code, name, description, is_system, created_at, updated_at
		FROM roles
		ORDER BY name
	`

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()

	var roles []auth.Role
	for rows.Next() {
		var role auth.Role
		err := rows.Scan(
			&role.ID, &role.Code, &role.Name,
			&role.Description, &role.IsSystem, &role.CreatedAt, &role.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, role)
	}

	return roles, nil
}

// LoadPermissions loads role's permissions.
func (r *RoleRepo) LoadPermissions(ctx context.Context, roleID id.ID) ([]auth.Permission, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT p.id, p.code, p.name, p.description, p.resource, p.action, p.created_at
		FROM permissions p
		INNER JOIN role_permissions rp ON p.id = rp.permission_id
		WHERE rp.role_id = $1
	`

	rows, err := q.Query(ctx, query, roleID)
	if err != nil {
		return nil, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()

	var permissions []auth.Permission
	for rows.Next() {
		var perm auth.Permission
		err := rows.Scan(
			&perm.ID, &perm.Code, &perm.Name, &perm.Description,
			&perm.Resource, &perm.Action, &perm.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

// AssignPermission assigns a permission to role.
func (r *RoleRepo) AssignPermission(ctx context.Context, roleID, permissionID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO role_permissions (role_id, permission_id, created_at)
		VALUES ($1, $2, now())
		ON CONFLICT (role_id, permission_id) DO NOTHING
	`

	_, err := q.Exec(ctx, query, roleID, permissionID)
	if err != nil {
		return fmt.Errorf("assign permission: %w", err)
	}

	return nil
}

// RevokePermission revokes a permission from role.
func (r *RoleRepo) RevokePermission(ctx context.Context, roleID, permissionID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `DELETE FROM role_permissions WHERE role_id = $1 AND permission_id = $2`
	_, err := q.Exec(ctx, query, roleID, permissionID)
	if err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}

	return nil
}

// Ensure interface compliance
var _ auth.RoleRepository = (*RoleRepo)(nil)

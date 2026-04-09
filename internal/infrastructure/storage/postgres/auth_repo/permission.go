// Package auth_repo provides PostgreSQL implementations for auth repositories.
// In Database-per-Tenant architecture, TxManager is obtained from context.
package auth_repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/auth"
	"metapus/internal/infrastructure/storage/postgres"
)

// PermissionRepo implements auth.PermissionRepository.
// In Database-per-Tenant, TxManager is obtained from context.
type PermissionRepo struct{}

// NewPermissionRepo creates a new permission repository.
func NewPermissionRepo() *PermissionRepo {
	return &PermissionRepo{}
}

// getTxManager retrieves TxManager from context.
func (r *PermissionRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// GetByCode retrieves permission by code.
func (r *PermissionRepo) GetByCode(ctx context.Context, code string) (*auth.Permission, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, code, name, description, resource, action
		FROM permissions WHERE code = $1
	`

	var perm auth.Permission
	err := q.QueryRow(ctx, query, code).Scan(
		&perm.ID, &perm.Code, &perm.Name, &perm.Description,
		&perm.Resource, &perm.Action,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("permission", code)
	}
	if err != nil {
		return nil, fmt.Errorf("query permission: %w", err)
	}

	return &perm, nil
}

// List retrieves all permissions.
func (r *PermissionRepo) List(ctx context.Context) ([]auth.Permission, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, code, name, description, resource, action
		FROM permissions ORDER BY resource, action
	`

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()

	var permissions []auth.Permission
	for rows.Next() {
		var perm auth.Permission
		err := rows.Scan(
			&perm.ID, &perm.Code, &perm.Name, &perm.Description,
			&perm.Resource, &perm.Action,
		)
		if err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

// ListByResource retrieves permissions for a resource.
func (r *PermissionRepo) ListByResource(ctx context.Context, resource string) ([]auth.Permission, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, code, name, description, resource, action
		FROM permissions WHERE resource = $1 ORDER BY action
	`

	rows, err := q.Query(ctx, query, resource)
	if err != nil {
		return nil, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()

	var permissions []auth.Permission
	for rows.Next() {
		var perm auth.Permission
		err := rows.Scan(
			&perm.ID, &perm.Code, &perm.Name, &perm.Description,
			&perm.Resource, &perm.Action,
		)
		if err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		permissions = append(permissions, perm)
	}

	return permissions, nil
}

// Ensure interface compliance
var _ auth.PermissionRepository = (*PermissionRepo)(nil)

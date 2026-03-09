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

// UserRepo implements auth.UserRepository.
// In Database-per-Tenant, TxManager is obtained from context.
type UserRepo struct{}

// NewUserRepo creates a new user repository.
func NewUserRepo() *UserRepo {
	return &UserRepo{}
}

// getTxManager retrieves TxManager from context.
func (r *UserRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// Create creates a new user.
func (r *UserRepo) Create(ctx context.Context, user *auth.User) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO users (
			id, email, password_hash, first_name, last_name,
			is_active, is_admin, email_verified, version, deletion_mark, attributes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := q.Exec(ctx, query,
		user.ID, user.Email, user.PasswordHash,
		user.FirstName, user.LastName, user.IsActive, user.IsAdmin,
		user.EmailVerified, user.Version, user.DeletionMark, user.Attributes,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}

	return nil
}

// GetByID retrieves user by ID.
func (r *UserRepo) GetByID(ctx context.Context, userID id.ID) (*auth.User, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, email, password_hash, first_name, last_name,
			   is_active, is_admin, email_verified, email_verified_at,
			   last_login_at, failed_login_attempts, locked_until,
			   deletion_mark, version, attributes
		FROM users
		WHERE id = $1 AND deletion_mark = FALSE
	`

	var user auth.User
	err := q.QueryRow(ctx, query, userID).Scan(
		&user.ID, &user.Email, &user.PasswordHash,
		&user.FirstName, &user.LastName, &user.IsActive, &user.IsAdmin,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.LastLoginAt,
		&user.FailedLoginAttempts, &user.LockedUntil,
		&user.DeletionMark, &user.Version, &user.Attributes,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("user", userID.String())
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

// GetByEmail retrieves user by email (within tenant database).
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*auth.User, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, email, password_hash, first_name, last_name,
			   is_active, is_admin, email_verified, email_verified_at,
			   last_login_at, failed_login_attempts, locked_until,
			   deletion_mark, version, attributes
		FROM users
		WHERE email = $1 AND deletion_mark = FALSE
	`

	var user auth.User
	err := q.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash,
		&user.FirstName, &user.LastName, &user.IsActive, &user.IsAdmin,
		&user.EmailVerified, &user.EmailVerifiedAt, &user.LastLoginAt,
		&user.FailedLoginAttempts, &user.LockedUntil,
		&user.DeletionMark, &user.Version, &user.Attributes,
	)
	if err == pgx.ErrNoRows {
		return nil, apperror.NewNotFound("user", email)
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}

	return &user, nil
}

// Update updates user data.
func (r *UserRepo) Update(ctx context.Context, user *auth.User) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		UPDATE users SET
			first_name = $2,
			last_name = $3,
			is_active = $4,
			is_admin = $5,
			email_verified = $6,
			email_verified_at = $7,
			last_login_at = $8,
			failed_login_attempts = $9,
			locked_until = $10,
			version = version + 1,
			deletion_mark = $11,
			attributes = $12
		WHERE id = $1 AND deletion_mark = FALSE AND version = $13
	`

	result, err := q.Exec(ctx, query,
		user.ID, user.FirstName, user.LastName, user.IsActive, user.IsAdmin,
		user.EmailVerified, user.EmailVerifiedAt, user.LastLoginAt,
		user.FailedLoginAttempts, user.LockedUntil, user.DeletionMark, user.Attributes,
		user.Version,
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewConcurrentModification("user", user.ID)
	}

	user.Version++
	return nil
}

// Delete soft-deletes a user.
func (r *UserRepo) Delete(ctx context.Context, userID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `UPDATE users SET deletion_mark = TRUE WHERE id = $1 AND deletion_mark = FALSE`
	result, err := q.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return apperror.NewNotFound("user", userID.String())
	}

	return nil
}

// List retrieves users with filtering.
func (r *UserRepo) List(ctx context.Context, filter auth.UserFilter) ([]auth.User, int, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT id, email, password_hash, first_name, last_name,
			   is_active, is_admin, email_verified, email_verified_at,
			   last_login_at, deletion_mark, version, attributes
		FROM users
		WHERE deletion_mark = FALSE
	`
	countQuery := `SELECT COUNT(*) FROM users WHERE deletion_mark = FALSE`

	var args []interface{}
	argIdx := 1

	if filter.Search != "" {
		query += fmt.Sprintf(" AND (email ILIKE $%d OR first_name ILIKE $%d OR last_name ILIKE $%d)", argIdx, argIdx, argIdx)
		countQuery += fmt.Sprintf(" AND (email ILIKE $%d OR first_name ILIKE $%d OR last_name ILIKE $%d)", argIdx, argIdx, argIdx)
		args = append(args, "%"+filter.Search+"%")
		argIdx++
	}

	if filter.IsActive != nil {
		query += fmt.Sprintf(" AND is_active = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND is_active = $%d", argIdx)
		args = append(args, *filter.IsActive)
		argIdx++
	}

	// Get total count
	var total int
	err := q.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	// Add pagination
	query += " ORDER BY id ASC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []auth.User
	for rows.Next() {
		var user auth.User
		err := rows.Scan(
			&user.ID, &user.Email, &user.PasswordHash,
			&user.FirstName, &user.LastName, &user.IsActive, &user.IsAdmin,
			&user.EmailVerified, &user.EmailVerifiedAt, &user.LastLoginAt,
			&user.DeletionMark, &user.Version, &user.Attributes,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}

	return users, total, nil
}

// LoadRoles loads user's roles.
func (r *UserRepo) LoadRoles(ctx context.Context, userID id.ID) ([]auth.Role, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT r.id, r.code, r.name, r.description, r.is_system
		FROM roles r
		INNER JOIN user_roles ur ON r.id = ur.role_id
		WHERE ur.user_id = $1
	`

	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()

	var roles []auth.Role
	for rows.Next() {
		var role auth.Role
		err := rows.Scan(
			&role.ID, &role.Code, &role.Name,
			&role.Description, &role.IsSystem,
		)
		if err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, role)
	}

	return roles, nil
}

// LoadPermissions loads user's permissions (flattened from roles).
func (r *UserRepo) LoadPermissions(ctx context.Context, userID id.ID) ([]string, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		SELECT DISTINCT p.code
		FROM permissions p
		INNER JOIN role_permissions rp ON p.id = rp.permission_id
		INNER JOIN user_roles ur ON rp.role_id = ur.role_id
		WHERE ur.user_id = $1
	`

	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()

	var permissions []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		permissions = append(permissions, code)
	}

	return permissions, nil
}

// LoadOrganizations loads user's organization IDs.
func (r *UserRepo) LoadOrganizations(ctx context.Context, userID id.ID) ([]string, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `SELECT organization_id FROM user_organizations WHERE user_id = $1`

	rows, err := q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query organizations: %w", err)
	}
	defer rows.Close()

	var orgIDs []string
	for rows.Next() {
		var orgID string
		if err := rows.Scan(&orgID); err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		orgIDs = append(orgIDs, orgID)
	}

	return orgIDs, nil
}

// AssignRole assigns a role to user.
func (r *UserRepo) AssignRole(ctx context.Context, userID, roleID id.ID, grantedBy id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO user_roles (user_id, role_id, granted_by)
		VALUES ($1, $2, NULLIF($3, '00000000-0000-0000-0000-000000000000'::uuid))
		ON CONFLICT (user_id, role_id) DO NOTHING
	`

	_, err := q.Exec(ctx, query, userID, roleID, grantedBy)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}

	return nil
}

// RevokeRole revokes a role from user.
func (r *UserRepo) RevokeRole(ctx context.Context, userID, roleID id.ID) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`
	_, err := q.Exec(ctx, query, userID, roleID)
	if err != nil {
		return fmt.Errorf("revoke role: %w", err)
	}

	return nil
}

// Exists checks if email exists (within tenant database).
func (r *UserRepo) Exists(ctx context.Context, email string) (bool, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `SELECT EXISTS(SELECT 1 FROM users WHERE email = $1 AND deletion_mark = FALSE)`

	var exists bool
	err := q.QueryRow(ctx, query, email).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check exists: %w", err)
	}

	return exists, nil
}

// Ensure interface compliance
var _ auth.UserRepository = (*UserRepo)(nil)

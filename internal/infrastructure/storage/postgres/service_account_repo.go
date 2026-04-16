package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/integrations"
)

// ServiceAccountRepo implements both integrations.Repository and integrations.CredentialManager
type ServiceAccountRepo struct{}

// NewServiceAccountRepo creates a new repository.
func NewServiceAccountRepo() *ServiceAccountRepo {
	return &ServiceAccountRepo{}
}

// getEncryptionKey retrieves the key or returns an error if not configured.
func (r *ServiceAccountRepo) getEncryptionKey() (string, error) {
	key := os.Getenv("METAPUS_CREDENTIALS_KEY")
	if key == "" {
		return "", fmt.Errorf("METAPUS_CREDENTIALS_KEY environment variable is not set")
	}
	return key, nil
}

// List returns all service accounts.
func (r *ServiceAccountRepo) List(ctx context.Context) ([]integrations.ServiceAccount, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT id, name, account_type, config, organization_id, status, is_default, last_error, last_success_at, created_at, updated_at
		FROM sys_service_accounts
		ORDER BY created_at DESC
	`

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query list service accounts: %w", err)
	}
	defer rows.Close()

	var accounts []integrations.ServiceAccount
	for rows.Next() {
		var sa integrations.ServiceAccount
		var configBytes []byte
		err := rows.Scan(
			&sa.ID, &sa.Name, &sa.AccountType, &configBytes, &sa.OrganizationID,
			&sa.Status, &sa.IsDefault, &sa.LastError, &sa.LastSuccessAt,
			&sa.CreatedAt, &sa.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan service account: %w", err)
		}
		if err := json.Unmarshal(configBytes, &sa.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
		accounts = append(accounts, sa)
	}

	return accounts, nil
}

// GetByID retrieves a service account by ID.
func (r *ServiceAccountRepo) GetByID(ctx context.Context, accountID id.ID) (*integrations.ServiceAccount, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT id, name, account_type, config, organization_id, status, is_default, last_error, last_success_at, created_at, updated_at
		FROM sys_service_accounts
		WHERE id = $1
	`

	var sa integrations.ServiceAccount
	var configBytes []byte
	err := q.QueryRow(ctx, query, accountID).Scan(
		&sa.ID, &sa.Name, &sa.AccountType, &configBytes, &sa.OrganizationID,
		&sa.Status, &sa.IsDefault, &sa.LastError, &sa.LastSuccessAt,
		&sa.CreatedAt, &sa.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_service_accounts", accountID)
		}
		return nil, fmt.Errorf("query service account by id: %w", err)
	}

	if err := json.Unmarshal(configBytes, &sa.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	return &sa, nil
}

// Create creates a new service account.
func (r *ServiceAccountRepo) Create(ctx context.Context, req integrations.CreateRequest) (*integrations.ServiceAccount, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	key, keyErr := r.getEncryptionKey()
	if keyErr != nil && len(req.Credentials) > 0 {
		return nil, apperror.NewInternal(errors.New("encryption key not configured"))
	}

	query := `
		INSERT INTO sys_service_accounts (
			name, account_type, config, organization_id, status, is_default, credentials_enc
		) VALUES (
			$1, $2, $3, $4, 'active', $5,
			CASE WHEN $6::bytea IS NOT NULL THEN pgp_sym_encrypt($6::text, $7) ELSE NULL END
		)
		RETURNING id, name, account_type, config, organization_id, status, is_default, created_at, updated_at
	`

	var sa integrations.ServiceAccount
	var retConfigBytes []byte
	var credentialsArg interface{}
	if len(req.Credentials) > 0 {
		credentialsArg = req.Credentials
	}

	err = q.QueryRow(ctx, query,
		req.Name, req.AccountType, configBytes, req.OrganizationID, req.IsDefault, credentialsArg, key,
	).Scan(
		&sa.ID, &sa.Name, &sa.AccountType, &retConfigBytes, &sa.OrganizationID,
		&sa.Status, &sa.IsDefault, &sa.CreatedAt, &sa.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.NewValidation("A default account for this type and organization already exists.")
		}
		return nil, fmt.Errorf("create service account: %w", err)
	}

	if err := json.Unmarshal(retConfigBytes, &sa.Config); err != nil {
		return nil, fmt.Errorf("unmarshal return config: %w", err)
	}

	return &sa, nil
}

// Update modifies an existing service account.
func (r *ServiceAccountRepo) Update(ctx context.Context, accountID id.ID, req integrations.UpdateRequest) (*integrations.ServiceAccount, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	query := `
		UPDATE sys_service_accounts
		SET name = $1, config = $2, organization_id = $3, status = $4, is_default = $5, updated_at = NOW()
		WHERE id = $6
		RETURNING id, name, account_type, config, organization_id, status, is_default, last_error, last_success_at, created_at, updated_at
	`

	var sa integrations.ServiceAccount
	var retConfigBytes []byte
	err = q.QueryRow(ctx, query,
		req.Name, configBytes, req.OrganizationID, req.Status, req.IsDefault, accountID,
	).Scan(
		&sa.ID, &sa.Name, &sa.AccountType, &retConfigBytes, &sa.OrganizationID,
		&sa.Status, &sa.IsDefault, &sa.LastError, &sa.LastSuccessAt,
		&sa.CreatedAt, &sa.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_service_accounts", accountID)
		}
		if isUniqueViolation(err) {
			return nil, apperror.NewValidation("A default account for this type and organization already exists.")
		}
		return nil, fmt.Errorf("update service account: %w", err)
	}

	if err := json.Unmarshal(retConfigBytes, &sa.Config); err != nil {
		return nil, fmt.Errorf("unmarshal return config: %w", err)
	}

	return &sa, nil
}

// Delete removes a service account.
func (r *ServiceAccountRepo) Delete(ctx context.Context, accountID id.ID) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `DELETE FROM sys_service_accounts WHERE id = $1`
	cmdTag, err := q.Exec(ctx, query, accountID)
	if err != nil {
		return fmt.Errorf("delete service account: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_service_accounts", accountID)
	}

	return nil
}

// UpdateStatus updates the operational status.
func (r *ServiceAccountRepo) UpdateStatus(ctx context.Context, accountID id.ID, status integrations.AccountStatus, lastError *string, success bool) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		UPDATE sys_service_accounts
		SET status = $1, last_error = $2, updated_at = NOW(),
		    last_success_at = CASE WHEN $3 = TRUE THEN NOW() ELSE last_success_at END
		WHERE id = $4
	`
	cmdTag, err := q.Exec(ctx, query, status, lastError, success, accountID)
	if err != nil {
		return fmt.Errorf("update service account status: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_service_accounts", accountID)
	}

	return nil
}

// WriteCredentials encrypts and saves credentials.
func (r *ServiceAccountRepo) WriteCredentials(ctx context.Context, accountID id.ID, credentials []byte) error {
	key, err := r.getEncryptionKey()
	if err != nil {
		return apperror.NewInternal(errors.New("encryption key not configured"))
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	var credentialsArg interface{}
	if len(credentials) > 0 {
		credentialsArg = credentials
	}

	query := `
		UPDATE sys_service_accounts
		SET credentials_enc = CASE WHEN $1::bytea IS NOT NULL THEN pgp_sym_encrypt($1::text, $2) ELSE NULL END,
			updated_at = NOW()
		WHERE id = $3
	`

	cmdTag, dbErr := q.Exec(ctx, query, credentialsArg, key, accountID)
	if dbErr != nil {
		return fmt.Errorf("write credentials: %w", dbErr)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_service_accounts", accountID)
	}

	return nil
}

// ReadCredentials retrieves and decrypts credentials.
func (r *ServiceAccountRepo) ReadCredentials(ctx context.Context, accountID id.ID) ([]byte, error) {
	key, err := r.getEncryptionKey()
	if err != nil {
		return nil, apperror.NewInternal(errors.New("encryption key not configured"))
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT CASE WHEN credentials_enc IS NOT NULL THEN pgp_sym_decrypt(credentials_enc, $1) ELSE NULL END
		FROM sys_service_accounts
		WHERE id = $2
	`

	var decrypted *string
	dbErr := q.QueryRow(ctx, query, key, accountID).Scan(&decrypted)
	if dbErr != nil {
		if dbErr == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_service_accounts", accountID)
		}
		return nil, fmt.Errorf("read credentials: %w", dbErr)
	}

	if decrypted == nil {
		return nil, nil // No credentials set
	}

	return []byte(*decrypted), nil
}

// isUniqueViolation checks whether the error is a Postgres unique constraint violation
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// Simple check based on error string since we might not always have direct access to pgconn under wrappers
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key value")
}

// Ensure interface compliance
var _ integrations.Repository = (*ServiceAccountRepo)(nil)
var _ integrations.CredentialManager = (*ServiceAccountRepo)(nil)

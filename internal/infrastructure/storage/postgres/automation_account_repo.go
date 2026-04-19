package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/crypto"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// AutomationAccountRepo implements automations.AccountRepository and automations.CredentialManager.
type AutomationAccountRepo struct {
	encryptionKey []byte
}

// NewAutomationAccountRepo creates a new repository.
// Encryption key is loaded from AUTOMATION_ENCRYPTION_KEY env var on first use.
func NewAutomationAccountRepo() *AutomationAccountRepo {
	return &AutomationAccountRepo{}
}

func (r *AutomationAccountRepo) getEncryptionKey() ([]byte, error) {
	if len(r.encryptionKey) > 0 {
		return r.encryptionKey, nil
	}
	keyStr := os.Getenv("AUTOMATION_ENCRYPTION_KEY")
	if keyStr == "" {
		return nil, fmt.Errorf("AUTOMATION_ENCRYPTION_KEY environment variable is not set")
	}
	key := []byte(keyStr)
	if len(key) != 32 {
		return nil, fmt.Errorf("AUTOMATION_ENCRYPTION_KEY must be 32 bytes, got %d", len(key))
	}
	r.encryptionKey = key
	return key, nil
}

// scanAccount is a helper to scan a row into an Account struct.
func scanAccount(row pgx.Row) (*automations.Account, error) {
	var a automations.Account
	var configBytes []byte
	err := row.Scan(
		&a.ID, &a.Name, &a.AccountType, &configBytes,
		&a.OrganizationID, &a.IsActive, &a.Status,
		&a.LastError, &a.LastSuccessAt,
		&a.DeletionMark, &a.Version, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(configBytes, &a.Config); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &a, nil
}

const accountSelectCols = `id, name, account_type, config, organization_id, is_active, status, last_error, last_success_at, deletion_mark, version, created_at, updated_at`

// List returns all non-deleted automation accounts.
func (r *AutomationAccountRepo) List(ctx context.Context) ([]automations.Account, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`
		SELECT %s,
			(SELECT COUNT(*) FROM sys_automation_channels c WHERE c.account_id = a.id AND c.deletion_mark = FALSE) AS channel_count
		FROM sys_automation_accounts a
		WHERE a.deletion_mark = FALSE
		ORDER BY a.created_at DESC
	`, accountSelectCols)

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []automations.Account
	for rows.Next() {
		var a automations.Account
		var configBytes []byte
		err := rows.Scan(
			&a.ID, &a.Name, &a.AccountType, &configBytes,
			&a.OrganizationID, &a.IsActive, &a.Status,
			&a.LastError, &a.LastSuccessAt,
			&a.DeletionMark, &a.Version, &a.CreatedAt, &a.UpdatedAt,
			&a.ChannelCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		if err := json.Unmarshal(configBytes, &a.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
		accounts = append(accounts, a)
	}

	return accounts, nil
}

// GetByID retrieves an account by ID.
func (r *AutomationAccountRepo) GetByID(ctx context.Context, accountID id.ID) (*automations.Account, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := fmt.Sprintf(`SELECT %s FROM sys_automation_accounts WHERE id = $1 AND deletion_mark = FALSE`, accountSelectCols)

	a, err := scanAccount(q.QueryRow(ctx, query, accountID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_automation_accounts", accountID)
		}
		return nil, fmt.Errorf("query account by id: %w", err)
	}

	return a, nil
}

// Create creates a new account. Credentials are encrypted with AES-256-GCM.
func (r *AutomationAccountRepo) Create(ctx context.Context, req automations.CreateAccountRequest) (*automations.Account, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	// Encrypt credentials at application level
	var credEnc []byte
	if req.Credentials != "" {
		key, keyErr := r.getEncryptionKey()
		if keyErr != nil {
			return nil, apperror.NewInternal(keyErr)
		}
		credEnc, err = crypto.Encrypt([]byte(req.Credentials), key)
		if err != nil {
			return nil, apperror.NewInternal(fmt.Errorf("encrypt credentials: %w", err))
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO sys_automation_accounts (
			name, account_type, config, organization_id, is_active, credentials_enc
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING %s
	`, accountSelectCols)

	a, err := scanAccount(q.QueryRow(ctx, query,
		req.Name, req.AccountType, configBytes, req.OrganizationID, req.IsActive, credEnc,
	))
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, apperror.NewValidation("Account with this code already exists.")
		}
		return nil, fmt.Errorf("create automation account: %w", err)
	}

	return a, nil
}

// Update modifies an existing account (excluding credentials).
func (r *AutomationAccountRepo) Update(ctx context.Context, accountID id.ID, req automations.UpdateAccountRequest) (*automations.Account, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	query := fmt.Sprintf(`
		UPDATE sys_automation_accounts
		SET name = $1, config = $2, organization_id = $3, is_active = $4, version = version + 1
		WHERE id = $5 AND version = $6 AND deletion_mark = FALSE
		RETURNING %s
	`, accountSelectCols)

	a, err := scanAccount(q.QueryRow(ctx, query,
		req.Name, configBytes, req.OrganizationID, req.IsActive,
		accountID, req.Version,
	))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewConcurrentModification("sys_automation_accounts", accountID)
		}
		return nil, fmt.Errorf("update automation account: %w", err)
	}

	return a, nil
}

// Delete marks an account for deletion (soft delete).
func (r *AutomationAccountRepo) Delete(ctx context.Context, accountID id.ID) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `UPDATE sys_automation_accounts SET deletion_mark = TRUE WHERE id = $1 AND deletion_mark = FALSE`
	cmdTag, err := q.Exec(ctx, query, accountID)
	if err != nil {
		if IsForeignKeyViolation(err) {
			return apperror.NewValidation("Cannot delete account with active channels. Remove channels first.")
		}
		return fmt.Errorf("delete automation account: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_automation_accounts", accountID)
	}

	return nil
}

// UpdateLastResult updates status/error/success fields after delivery.
func (r *AutomationAccountRepo) UpdateLastResult(ctx context.Context, accountID id.ID, success bool, lastErr *string) error {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		UPDATE sys_automation_accounts
		SET status = CASE WHEN $2 THEN 'active'::text ELSE 'error'::text END,
			last_error = CASE WHEN $2 THEN NULL ELSE $3 END,
			last_success_at = CASE WHEN $2 THEN NOW() ELSE last_success_at END
		WHERE id = $1
	`

	_, err := q.Exec(ctx, query, accountID, success, lastErr)
	return err
}

// WriteCredentials encrypts and saves credentials for an account.
func (r *AutomationAccountRepo) WriteCredentials(ctx context.Context, accountID id.ID, credentials []byte) error {
	key, err := r.getEncryptionKey()
	if err != nil {
		return apperror.NewInternal(err)
	}

	credEnc, err := crypto.Encrypt(credentials, key)
	if err != nil {
		return apperror.NewInternal(fmt.Errorf("encrypt credentials: %w", err))
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `UPDATE sys_automation_accounts SET credentials_enc = $1 WHERE id = $2 AND deletion_mark = FALSE`
	cmdTag, err := q.Exec(ctx, query, credEnc, accountID)
	if err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	if cmdTag.RowsAffected() == 0 {
		return apperror.NewNotFound("sys_automation_accounts", accountID)
	}

	return nil
}

// ReadCredentials retrieves and decrypts credentials for an account.
func (r *AutomationAccountRepo) ReadCredentials(ctx context.Context, accountID id.ID) ([]byte, error) {
	key, err := r.getEncryptionKey()
	if err != nil {
		return nil, apperror.NewInternal(err)
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `SELECT credentials_enc FROM sys_automation_accounts WHERE id = $1 AND deletion_mark = FALSE`
	var credEnc []byte
	if err := q.QueryRow(ctx, query, accountID).Scan(&credEnc); err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewNotFound("sys_automation_accounts", accountID)
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	if credEnc == nil {
		return nil, nil // No credentials stored
	}

	plaintext, err := crypto.Decrypt(credEnc, key)
	if err != nil {
		return nil, apperror.NewInternal(fmt.Errorf("decrypt credentials: %w", err))
	}

	return plaintext, nil
}

// Ensure interface compliance
var _ automations.AccountRepository = (*AutomationAccountRepo)(nil)
var _ automations.CredentialManager = (*AutomationAccountRepo)(nil)

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/settings"
)

// SettingsRepo implements settings.Repository using the tenant database.
type SettingsRepo struct{}

// NewSettingsRepo creates a new settings repository.
func NewSettingsRepo() *SettingsRepo {
	return &SettingsRepo{}
}

// validSections is the whitelist of updatable JSONB columns.
var validSections = map[string]bool{
	"organization": true,
	"accounting":   true,
	"performance":  true,
}

// Get returns the current settings from sys_settings (single-row table).
func (r *SettingsRepo) Get(ctx context.Context) (*settings.Settings, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `
		SELECT organization, accounting, performance, version, updated_at
		FROM sys_settings
		WHERE singleton = TRUE
	`

	var orgJSON, accJSON, perfJSON []byte
	var s settings.Settings

	err := q.QueryRow(ctx, query).Scan(
		&orgJSON, &accJSON, &perfJSON,
		&s.Version, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("query sys_settings: %w", err)
	}

	if err := json.Unmarshal(orgJSON, &s.Organization); err != nil {
		return nil, fmt.Errorf("unmarshal organization: %w", err)
	}
	if err := json.Unmarshal(accJSON, &s.Accounting); err != nil {
		return nil, fmt.Errorf("unmarshal accounting: %w", err)
	}
	if err := json.Unmarshal(perfJSON, &s.Performance); err != nil {
		return nil, fmt.Errorf("unmarshal performance: %w", err)
	}

	return &s, nil
}

// UpdateSection updates a single JSONB section with optimistic locking.
// Returns apperror.ErrConcurrentModification if version does not match.
func (r *SettingsRepo) UpdateSection(ctx context.Context, section string, data json.RawMessage, version int) (*settings.Settings, error) {
	if !validSections[section] {
		return nil, apperror.NewValidation("invalid settings section: " + section)
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	// Dynamic column name is safe here because section is validated against whitelist.
	query := fmt.Sprintf(`
		UPDATE sys_settings
		SET %s = $1,
		    version = version + 1,
		    updated_at = NOW()
		WHERE singleton = TRUE AND version = $2
		RETURNING organization, accounting, performance, version, updated_at
	`, section)

	var orgJSON, accJSON, perfJSON []byte
	var s settings.Settings

	err := q.QueryRow(ctx, query, data, version).Scan(
		&orgJSON, &accJSON, &perfJSON,
		&s.Version, &s.UpdatedAt,
	)
	if err != nil {
		// pgx returns ErrNoRows when WHERE version = $2 doesn't match
		if err.Error() == "no rows in result set" {
			return nil, apperror.NewConcurrentModification("sys_settings", "singleton")
		}
		return nil, fmt.Errorf("update sys_settings.%s: %w", section, err)
	}

	if err := json.Unmarshal(orgJSON, &s.Organization); err != nil {
		return nil, fmt.Errorf("unmarshal organization: %w", err)
	}
	if err := json.Unmarshal(accJSON, &s.Accounting); err != nil {
		return nil, fmt.Errorf("unmarshal accounting: %w", err)
	}
	if err := json.Unmarshal(perfJSON, &s.Performance); err != nil {
		return nil, fmt.Errorf("unmarshal performance: %w", err)
	}

	return &s, nil
}

// Ensure interface compliance.
var _ settings.Repository = (*SettingsRepo)(nil)

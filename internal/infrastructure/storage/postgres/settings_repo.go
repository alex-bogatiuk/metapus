package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/json"
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
	"general":       true,
	"numbering":     true,
	"performance":   true,
	"exchangeRates": true,
}

// allColumns lists all JSONB setting columns in scan order.
const settingsSelectCols = `general, numbering, performance, exchange_rates, version, updated_at`

// Get returns the current settings from sys_settings (single-row table).
func (r *SettingsRepo) Get(ctx context.Context) (*settings.Settings, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `SELECT ` + settingsSelectCols + ` FROM sys_settings WHERE singleton = TRUE`

	var genJSON, numJSON, perfJSON, ratesJSON []byte
	var s settings.Settings

	err := q.QueryRow(ctx, query).Scan(
		&genJSON, &numJSON, &perfJSON, &ratesJSON,
		&s.Version, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("query sys_settings: %w", err)
	}

	if err := json.Unmarshal(genJSON, &s.General); err != nil {
		return nil, fmt.Errorf("unmarshal general: %w", err)
	}
	if err := json.Unmarshal(numJSON, &s.Numbering); err != nil {
		return nil, fmt.Errorf("unmarshal numbering: %w", err)
	}
	if err := json.Unmarshal(perfJSON, &s.Performance); err != nil {
		return nil, fmt.Errorf("unmarshal performance: %w", err)
	}

	if err := json.Unmarshal(ratesJSON, &s.ExchangeRates); err != nil {
		return nil, fmt.Errorf("unmarshal exchange rates settings: %w", err)
	}
	return &s, nil
}

// UpdateSection updates a single JSONB section with optimistic locking.
// Returns apperror.ErrConcurrentModification if version does not match.
func (r *SettingsRepo) UpdateSection(ctx context.Context, section string, data json.RawMessage, version int) (*settings.Settings, error) {
	var out *settings.Settings
	err := MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		var err error
		out, err = r.updateSection(ctx, section, data, version)
		return err
	})
	return out, err
}
func (r *SettingsRepo) updateSection(ctx context.Context, section string, data json.RawMessage, version int) (*settings.Settings, error) {
	if !validSections[section] {
		return nil, apperror.NewValidation("invalid settings section: " + section)
	}

	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	if section == "exchangeRates" {
		var rates settings.ExchangeRatesSettings
		if err := json.Unmarshal(data, &rates); err != nil {
			return nil, apperror.NewInvalidRequest(err)
		}
		if rates.SourceID != nil {
			var valid bool
			if err := q.QueryRow(ctx, `SELECT source_type='coingecko' AND NOT deletion_mark AND _deleted_at IS NULL AND NOT is_folder FROM cat_rate_sources WHERE id=$1 FOR SHARE`, *rates.SourceID).Scan(&valid); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, apperror.NewValidation("Выберите существующий источник CoinGecko")
				}
				return nil, fmt.Errorf("validate rate source: %w", err)
			}
			if !valid {
				return nil, apperror.NewValidation("Выберите источник CoinGecko")
			}
		}
		var err error
		data, err = json.Marshal(rates)
		if err != nil {
			return nil, err
		}
		section = "exchange_rates"
	}
	// Dynamic column name is safe here because section is validated against whitelist.
	query := fmt.Sprintf(`
		UPDATE sys_settings
		SET %s = $1,
		    version = version + 1,
		    updated_at = NOW()
		WHERE singleton = TRUE AND version = $2
		RETURNING `+settingsSelectCols+`
	`, section)

	var genJSON, numJSON, perfJSON, ratesJSON []byte
	var s settings.Settings

	err := q.QueryRow(ctx, query, data, version).Scan(
		&genJSON, &numJSON, &perfJSON, &ratesJSON,
		&s.Version, &s.UpdatedAt,
	)
	if err != nil {
		// pgx returns ErrNoRows when WHERE version = $2 doesn't match
		if err.Error() == "no rows in result set" {
			return nil, apperror.NewConcurrentModification("sys_settings", "singleton")
		}
		return nil, fmt.Errorf("update sys_settings.%s: %w", section, err)
	}

	if err := json.Unmarshal(genJSON, &s.General); err != nil {
		return nil, fmt.Errorf("unmarshal general: %w", err)
	}
	if err := json.Unmarshal(numJSON, &s.Numbering); err != nil {
		return nil, fmt.Errorf("unmarshal numbering: %w", err)
	}
	if err := json.Unmarshal(perfJSON, &s.Performance); err != nil {
		return nil, fmt.Errorf("unmarshal performance: %w", err)
	}

	if err := json.Unmarshal(ratesJSON, &s.ExchangeRates); err != nil {
		return nil, fmt.Errorf("unmarshal exchange rates settings: %w", err)
	}
	return &s, nil
}

// Ensure interface compliance.
var _ settings.Repository = (*SettingsRepo)(nil)

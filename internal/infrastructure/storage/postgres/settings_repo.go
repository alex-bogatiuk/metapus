package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/settings"
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
	"performance":   true,
	"exchangeRates": true,
	"periodLock":    true,
}

// allColumns lists all JSONB setting columns in scan order.
const settingsSelectCols = `general, performance, exchange_rates, version, updated_at`

// Get returns the current settings from sys_settings (single-row table).
func (r *SettingsRepo) Get(ctx context.Context) (*settings.Settings, error) {
	txm := MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)

	query := `SELECT ` + settingsSelectCols + ` FROM sys_settings WHERE singleton = TRUE`

	var genJSON, perfJSON, ratesJSON []byte
	var s settings.Settings

	err := q.QueryRow(ctx, query).Scan(
		&genJSON, &perfJSON, &ratesJSON,
		&s.Version, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("query sys_settings: %w", err)
	}

	if err := json.Unmarshal(genJSON, &s.General); err != nil {
		return nil, fmt.Errorf("unmarshal general: %w", err)
	}
	if err := json.Unmarshal(perfJSON, &s.Performance); err != nil {
		return nil, fmt.Errorf("unmarshal performance: %w", err)
	}

	if err := json.Unmarshal(ratesJSON, &s.ExchangeRates); err != nil {
		return nil, fmt.Errorf("unmarshal exchange rates settings: %w", err)
	}
	if s.PeriodLock, err = readPeriodLock(ctx); err != nil {
		return nil, err
	}
	return &s, nil
}

// readPeriodLock reads the tenant's lock date (the row without an
// organization of sys_period_locks).
func readPeriodLock(ctx context.Context) (settings.PeriodLockSettings, error) {
	var lock settings.PeriodLockSettings
	err := MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
		`SELECT to_char(locked_until, 'YYYY-MM-DD') FROM sys_period_locks WHERE organization_id IS NULL`).Scan(&lock.Date)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return lock, fmt.Errorf("read period lock: %w", err)
	}
	return lock, nil
}

// writePeriodLock sets or clears the tenant's lock date.
func writePeriodLock(ctx context.Context, lock settings.PeriodLockSettings) error {
	q := MustGetTxManager(ctx).GetQuerier(ctx)
	if lock.Date == "" {
		_, err := q.Exec(ctx, `DELETE FROM sys_period_locks WHERE organization_id IS NULL`)
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO sys_period_locks (locked_until, updated_by) VALUES ($1::date, NULLIF($2, '')::uuid)
		ON CONFLICT ((COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid)))
		DO UPDATE SET locked_until = EXCLUDED.locked_until, updated_at = now(), updated_by = EXCLUDED.updated_by`,
		lock.Date, appctx.GetUserID(ctx))
	return err
}

// PeriodLock reads the tenant's lock date and timezone for the document
// repository's check; without a settings row nothing is closed.
func PeriodLock(ctx context.Context) (settings.PeriodLockSettings, string, error) {
	var lock settings.PeriodLockSettings
	var timezone string
	err := MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT COALESCE(to_char(l.locked_until, 'YYYY-MM-DD'), ''), COALESCE(s.general->>'timezone', 'UTC')
		FROM sys_settings s LEFT JOIN sys_period_locks l ON l.organization_id IS NULL
		WHERE s.singleton = TRUE`).Scan(&lock.Date, &timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return lock, "UTC", nil
	}
	if err != nil {
		return lock, "", fmt.Errorf("read period lock: %w", err)
	}
	return lock, timezone, nil
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
	// The period lock lives in sys_period_locks; the settings row only takes
	// the new version, so a concurrent write of any section still conflicts.
	// Dynamic column name is safe here because section is validated against whitelist.
	query := fmt.Sprintf(`
		UPDATE sys_settings
		SET %s = $1,
		    version = version + 1,
		    updated_at = NOW()
		WHERE singleton = TRUE AND version = $2
		RETURNING `+settingsSelectCols+`
	`, section)
	args := []any{data, version}
	if section == "periodLock" {
		var lock settings.PeriodLockSettings
		if err := json.Unmarshal(data, &lock); err != nil {
			return nil, apperror.NewInvalidRequest(err)
		}
		if err := lock.Validate(); err != nil {
			return nil, err
		}
		if err := writePeriodLock(ctx, lock); err != nil {
			return nil, fmt.Errorf("write period lock: %w", err)
		}
		query = `
			UPDATE sys_settings
			SET version = version + 1, updated_at = NOW()
			WHERE singleton = TRUE AND version = $1
			RETURNING ` + settingsSelectCols
		args = []any{version}
	}

	var genJSON, perfJSON, ratesJSON []byte
	var s settings.Settings

	err := q.QueryRow(ctx, query, args...).Scan(
		&genJSON, &perfJSON, &ratesJSON,
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
	if err := json.Unmarshal(perfJSON, &s.Performance); err != nil {
		return nil, fmt.Errorf("unmarshal performance: %w", err)
	}

	if err := json.Unmarshal(ratesJSON, &s.ExchangeRates); err != nil {
		return nil, fmt.Errorf("unmarshal exchange rates settings: %w", err)
	}
	if s.PeriodLock, err = readPeriodLock(ctx); err != nil {
		return nil, err
	}
	return &s, nil
}

// Ensure interface compliance.
var _ settings.Repository = (*SettingsRepo)(nil)

package auth_repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/domain/userpref"
	"metapus/internal/infrastructure/storage/postgres"
)

// UserPrefsRepo implements userpref.Repository.
type UserPrefsRepo struct{}

// NewUserPrefsRepo creates a new user preferences repository.
func NewUserPrefsRepo() *UserPrefsRepo {
	return &UserPrefsRepo{}
}

func (r *UserPrefsRepo) getTxManager(ctx context.Context) *postgres.TxManager {
	return postgres.MustGetTxManager(ctx)
}

// GetOrCreate returns user preferences, inserting empty defaults if absent.
func (r *UserPrefsRepo) GetOrCreate(ctx context.Context, userID id.ID) (*userpref.UserPreferences, error) {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO user_preferences (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`
	if _, err := q.Exec(ctx, query, userID); err != nil {
		return nil, fmt.Errorf("upsert user_preferences: %w", err)
	}

	selectQuery := `
		SELECT user_id, interface, list_filters, list_columns, updated_at
		FROM user_preferences
		WHERE user_id = $1
	`

	var p userpref.UserPreferences
	var ifaceJSON, filtersJSON, columnsJSON []byte

	err := q.QueryRow(ctx, selectQuery, userID).Scan(
		&p.UserID, &ifaceJSON, &filtersJSON, &columnsJSON, &p.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return userpref.NewDefault(userID), nil
		}
		return nil, fmt.Errorf("query user_preferences: %w", err)
	}

	if err := json.Unmarshal(ifaceJSON, &p.Interface); err != nil {
		return nil, fmt.Errorf("unmarshal interface prefs: %w", err)
	}
	p.ListFilters = filtersJSON
	p.ListColumns = columnsJSON

	return &p, nil
}

// SaveInterface atomically upserts the interface section.
func (r *UserPrefsRepo) SaveInterface(ctx context.Context, userID id.ID, prefs userpref.InterfacePrefs) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	ifaceJSON, err := json.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("marshal interface prefs: %w", err)
	}

	query := `
		INSERT INTO user_preferences (user_id, interface)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		SET interface = $2, updated_at = NOW()
	`

	if _, err := q.Exec(ctx, query, userID, ifaceJSON); err != nil {
		return fmt.Errorf("upsert interface prefs: %w", err)
	}

	return nil
}

// SaveListFilters upserts filters for a single entity type using JSONB merge.
func (r *UserPrefsRepo) SaveListFilters(ctx context.Context, userID id.ID, entityType string, data json.RawMessage) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO user_preferences (user_id, list_filters)
		VALUES ($1, jsonb_build_object($2::text, $3::jsonb))
		ON CONFLICT (user_id) DO UPDATE
		SET list_filters = user_preferences.list_filters || jsonb_build_object($2::text, $3::jsonb),
		    updated_at = NOW()
	`

	if _, err := q.Exec(ctx, query, userID, entityType, data); err != nil {
		return fmt.Errorf("upsert list_filters[%s]: %w", entityType, err)
	}

	return nil
}

// SaveListColumns upserts visible columns for a single entity type using JSONB merge.
func (r *UserPrefsRepo) SaveListColumns(ctx context.Context, userID id.ID, entityType string, data json.RawMessage) error {
	q := r.getTxManager(ctx).GetQuerier(ctx)

	query := `
		INSERT INTO user_preferences (user_id, list_columns)
		VALUES ($1, jsonb_build_object($2::text, $3::jsonb))
		ON CONFLICT (user_id) DO UPDATE
		SET list_columns = user_preferences.list_columns || jsonb_build_object($2::text, $3::jsonb),
		    updated_at = NOW()
	`

	if _, err := q.Exec(ctx, query, userID, entityType, data); err != nil {
		return fmt.Errorf("upsert list_columns[%s]: %w", entityType, err)
	}

	return nil
}

// Ensure interface compliance.
var _ userpref.Repository = (*UserPrefsRepo)(nil)

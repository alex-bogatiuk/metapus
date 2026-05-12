package catalog_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/catalogs/merchant"
	"metapus/internal/infrastructure/storage/postgres"
)

const _merchantAPIKeyTable = "cat_merchant_api_keys"

// MerchantAPIKeyRepo implements merchant.APIKeyRepository.
// Uses TxManager from context — fully tenant-aware.
type MerchantAPIKeyRepo struct{}

// NewMerchantAPIKeyRepo creates a new API key repository.
func NewMerchantAPIKeyRepo() *MerchantAPIKeyRepo {
	return &MerchantAPIKeyRepo{}
}

func (r *MerchantAPIKeyRepo) builder() squirrel.StatementBuilderType {
	return squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
}

func (r *MerchantAPIKeyRepo) querier(ctx context.Context) postgres.Querier {
	return postgres.MustGetTxManager(ctx).GetQuerier(ctx)
}

// scopesToTextArray converts []APIKeyScope to a pgx-compatible TextArray.
func scopesToTextArray(scopes []merchant.APIKeyScope) pgtype.Array[string] {
	strs := make([]string, len(scopes))
	for i, s := range scopes {
		strs[i] = string(s)
	}
	elems := make([]pgtype.Text, len(strs))
	for i, s := range strs {
		elems[i] = pgtype.Text{String: s, Valid: true}
	}
	return pgtype.Array[string]{
		Elements: strs,
		Dims:     []pgtype.ArrayDimension{{Length: int32(len(strs)), LowerBound: 1}},
		Valid:    true,
	}
}

// Create inserts a new API key. ID is set by the database (gen_random_uuid_v7).
func (r *MerchantAPIKeyRepo) Create(ctx context.Context, key *merchant.MerchantAPIKey) error {
	scopeStrs := make([]string, len(key.Scopes))
	for i, s := range key.Scopes {
		scopeStrs[i] = string(s)
	}

	const q = `
		INSERT INTO cat_merchant_api_keys
			(merchant_id, name, key_prefix, key_hash, scopes, is_active, expires_at, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`

	row := r.querier(ctx).QueryRow(ctx, q,
		key.MerchantID, key.Name, key.KeyPrefix, key.KeyHash,
		scopeStrs, key.IsActive, key.ExpiresAt, key.CreatedByUserID,
	)
	if err := row.Scan(&key.ID, &key.CreatedAt, &key.UpdatedAt); err != nil {
		return fmt.Errorf("merchant_api_key create: %w", err)
	}
	return nil
}

// GetByHash retrieves an active API key by its SHA-256 hex hash.
// Uses the partial index idx_merchant_api_keys_hash (WHERE is_active = TRUE).
func (r *MerchantAPIKeyRepo) GetByHash(ctx context.Context, keyHash string) (*merchant.MerchantAPIKey, error) {
	const q = `
		SELECT id, merchant_id, name, key_prefix, key_hash,
		       scopes, is_active, last_used_at, expires_at,
		       created_by_user_id, created_at, updated_at
		FROM cat_merchant_api_keys
		WHERE key_hash = $1 AND is_active = TRUE
		LIMIT 1`

	var key merchant.MerchantAPIKey
	var scopeStrings []string

	row := r.querier(ctx).QueryRow(ctx, q, keyHash)
	err := row.Scan(
		&key.ID, &key.MerchantID, &key.Name, &key.KeyPrefix, &key.KeyHash,
		&scopeStrings, &key.IsActive, &key.LastUsedAt, &key.ExpiresAt,
		&key.CreatedByUserID, &key.CreatedAt, &key.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperror.NewUnauthorized("invalid api key")
		}
		return nil, fmt.Errorf("merchant_api_key get_by_hash: %w", err)
	}

	key.Scopes = make([]merchant.APIKeyScope, len(scopeStrings))
	for i, s := range scopeStrings {
		key.Scopes[i] = merchant.APIKeyScope(s)
	}

	return &key, nil
}

// ListByMerchant returns all keys for a merchant, ordered newest first.
func (r *MerchantAPIKeyRepo) ListByMerchant(ctx context.Context, merchantID id.ID) ([]*merchant.MerchantAPIKey, error) {
	const q = `
		SELECT id, merchant_id, name, key_prefix,
		       scopes, is_active, last_used_at, expires_at,
		       created_by_user_id, created_at, updated_at
		FROM cat_merchant_api_keys
		WHERE merchant_id = $1
		ORDER BY created_at DESC`
	// key_hash intentionally excluded — never expose hash to API consumers.

	rows, err := r.querier(ctx).Query(ctx, q, merchantID)
	if err != nil {
		return nil, fmt.Errorf("merchant_api_key list: %w", err)
	}
	defer rows.Close()

	var keys []*merchant.MerchantAPIKey
	for rows.Next() {
		var key merchant.MerchantAPIKey
		var scopeStrings []string
		if err := rows.Scan(
			&key.ID, &key.MerchantID, &key.Name, &key.KeyPrefix,
			&scopeStrings, &key.IsActive, &key.LastUsedAt, &key.ExpiresAt,
			&key.CreatedByUserID, &key.CreatedAt, &key.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("merchant_api_key list scan: %w", err)
		}
		key.Scopes = make([]merchant.APIKeyScope, len(scopeStrings))
		for i, s := range scopeStrings {
			key.Scopes[i] = merchant.APIKeyScope(s)
		}
		keys = append(keys, &key)
	}
	return keys, rows.Err()
}

// Revoke marks a key as inactive. Returns NotFound if the key doesn't belong to merchantID.
func (r *MerchantAPIKeyRepo) Revoke(ctx context.Context, keyID, merchantID id.ID) error {
	q := r.builder().
		Update(_merchantAPIKeyTable).
		Set("is_active", false).
		Where(squirrel.Eq{"id": keyID, "merchant_id": merchantID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("merchant_api_key revoke build: %w", err)
	}

	ct, err := r.querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("merchant_api_key revoke: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return apperror.NewNotFound(_merchantAPIKeyTable, keyID.String())
	}
	return nil
}

// UpdateLastUsed records the time the key was last used.
// Caller should invoke this in a goroutine — it is best-effort.
func (r *MerchantAPIKeyRepo) UpdateLastUsed(ctx context.Context, keyID id.ID) error {
	q := r.builder().
		Update(_merchantAPIKeyTable).
		Set("last_used_at", time.Now()).
		Where(squirrel.Eq{"id": keyID})

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("merchant_api_key update_last_used build: %w", err)
	}

	_, err = r.querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("merchant_api_key update_last_used: %w", err)
	}
	return nil
}

// Ensure MerchantAPIKeyRepo implements the interface at compile time.
var _ merchant.APIKeyRepository = (*MerchantAPIKeyRepo)(nil)

// Verify pgxscan is imported for future use.
var _ = pgxscan.Get

// scopesToTextArray is used by future batch operations.
var _ = scopesToTextArray

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"metapus/internal/core/apperror"
)

// IdempotencyStatus represents the state of an idempotent operation.
type IdempotencyStatus string

const (
	IdempotencyStatusPending IdempotencyStatus = "pending"
	IdempotencyStatusSuccess IdempotencyStatus = "success"
	IdempotencyStatusFailed  IdempotencyStatus = "failed"
)

// IdempotencyRecord stores the result of an idempotent operation.
type IdempotencyRecord struct {
	Key         string            `db:"idempotency_key"`
	UserID      string            `db:"user_id"`
	Operation   string            `db:"operation"`
	Status      IdempotencyStatus `db:"status"`
	RequestHash string            `db:"request_hash"` // SHA256 of request body
	Response    []byte            `db:"response"`     // Cached response
	StatusCode  int               `db:"response_status"`
	ContentType string            `db:"response_content_type"`
	CreatedAt   time.Time         `db:"created_at"`
	UpdatedAt   time.Time         `db:"updated_at"`
	ExpiresAt   time.Time         `db:"expires_at"`
}

// IdempotencyReplay is the cached HTTP response for replay.
type IdempotencyReplay struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

// IdempotencyStore manages idempotency keys.
type IdempotencyStore struct {
	pool      *pgxpool.Pool
	txManager *TxManager
	ttl       time.Duration
}

// NewIdempotencyStore creates a new idempotency store.
func NewIdempotencyStore(pool *Pool, txManager *TxManager, ttl time.Duration) *IdempotencyStore {
	return &IdempotencyStore{
		pool:      pool.Pool, // Достаем внутренний pgxpool.Pool из обертки
		txManager: txManager,
		ttl:       ttl,
	}
}

// NewIdempotencyStoreFromRawPool creates a new idempotency store from raw pgxpool.Pool.
// Useful in Database-per-Tenant mode where pool is obtained from context.
func NewIdempotencyStoreFromRawPool(pool *pgxpool.Pool, txManager *TxManager, ttl time.Duration) *IdempotencyStore {
	return &IdempotencyStore{
		pool:      pool,
		txManager: txManager,
		ttl:       ttl,
	}
}

// AcquireKey attempts to acquire an idempotency key.
// Returns:
//   - (nil, nil) if key acquired successfully
//   - (cachedResponse, nil) if operation already completed (success or failed)
//   - (nil, error) if key is locked by another request
func (s *IdempotencyStore) AcquireKey(ctx context.Context, key, userID, operation, requestHash string) (*IdempotencyReplay, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.ttl)

	// Try to insert or get existing
	var record IdempotencyRecord
	err := s.txManager.GetQuerier(ctx).QueryRow(ctx, `
		INSERT INTO sys_idempotency (idempotency_key, user_id, operation, status, request_hash, created_at, updated_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6, $7)
		ON CONFLICT (idempotency_key) DO UPDATE SET
			updated_at = $6,
			expires_at = GREATEST(sys_idempotency.expires_at, $7)
		RETURNING idempotency_key, user_id, operation, status, request_hash, response, response_status, response_content_type, created_at, updated_at, expires_at
	`, key, userID, operation, IdempotencyStatusPending, requestHash, now, expiresAt).Scan(
		&record.Key, &record.UserID, &record.Operation, &record.Status,
		&record.RequestHash, &record.Response, &record.StatusCode, &record.ContentType,
		&record.CreatedAt, &record.UpdatedAt, &record.ExpiresAt,
	)

	if err != nil {
		return nil, fmt.Errorf("acquire idempotency key: %w", err)
	}

	// Key was just created by us
	if record.CreatedAt.Equal(now) || record.CreatedAt.After(now.Add(-time.Second)) {
		return nil, nil
	}

	// Key exists: protect against reuse for a different request.
	if record.UserID != userID || record.Operation != operation || record.RequestHash != requestHash {
		return nil, apperror.NewIdempotencyMismatch(key).
			WithDetail("stored_user_id", record.UserID).
			WithDetail("request_user_id", userID).
			WithDetail("stored_operation", record.Operation).
			WithDetail("request_operation", operation).
			WithDetail("stored_request_hash", record.RequestHash).
			WithDetail("request_request_hash", requestHash)
	}

	// Key exists - check status
	switch record.Status {
	case IdempotencyStatusSuccess:
		return &IdempotencyReplay{
			StatusCode:  normalizeReplayStatus(record.StatusCode),
			ContentType: normalizeReplayContentType(record.ContentType),
			Body:        record.Response,
		}, nil

	case IdempotencyStatusPending:
		// Check if stale (older than 1 minute = likely crashed request)
		if time.Since(record.UpdatedAt) > time.Minute {
			// Reclaim stale key
			_, err := s.txManager.GetQuerier(ctx).Exec(ctx, `
				UPDATE sys_idempotency 
				SET status = $1, updated_at = $2
				WHERE idempotency_key = $3 AND status = $4
			`, IdempotencyStatusPending, now, key, IdempotencyStatusPending)
			if err != nil {
				return nil, fmt.Errorf("reclaim stale key: %w", err)
			}
			return nil, nil
		}
		// Key is actively being processed
		return nil, apperror.NewIdempotencyConflict(key)

	case IdempotencyStatusFailed:
		return &IdempotencyReplay{
			StatusCode:  normalizeReplayStatus(record.StatusCode),
			ContentType: normalizeReplayContentType(record.ContentType),
			Body:        record.Response,
		}, nil
	}

	return nil, nil
}

// CompleteKey marks an idempotency key as completed with HTTP response.
func (s *IdempotencyStore) CompleteKey(ctx context.Context, key string, statusCode int, contentType string, response any) error {
	var responseBytes []byte
	if response != nil {
		b, err := json.Marshal(response)
		if err != nil {
			return fmt.Errorf("marshal response: %w", err)
		}
		responseBytes = b
	}

	_, err := s.txManager.GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_idempotency 
		SET status = $1,
		    response = $2,
		    response_status = $3,
		    response_content_type = $4,
		    updated_at = $5
		WHERE idempotency_key = $6
	`, IdempotencyStatusSuccess, responseBytes, statusCode, contentType, time.Now().UTC(), key)

	return err
}

// FailKey marks an idempotency key as failed with HTTP response.
func (s *IdempotencyStore) FailKey(ctx context.Context, key string, statusCode int, contentType string, response any) error {
	var responseBytes []byte
	if response != nil {
		b, err := json.Marshal(response)
		if err != nil {
			// Best-effort: fall back to a minimal error body to keep the key consistent.
			responseBytes, _ = json.Marshal(map[string]string{"error": err.Error()})
		} else {
			responseBytes = b
		}
	}

	_, execErr := s.txManager.GetQuerier(ctx).Exec(ctx, `
		UPDATE sys_idempotency 
		SET status = $1,
		    response = $2,
		    response_status = $3,
		    response_content_type = $4,
		    updated_at = $5
		WHERE idempotency_key = $6
	`, IdempotencyStatusFailed, responseBytes, statusCode, contentType, time.Now().UTC(), key)

	return execErr
}

func normalizeReplayStatus(status int) int {
	// If older records exist without status, default to 200 for JSON bodies.
	if status == 0 {
		return 200
	}
	return status
}

func normalizeReplayContentType(ct string) string {
	if ct == "" {
		return "application/json"
	}
	return ct
}

// CleanupExpired removes expired idempotency records.
func (s *IdempotencyStore) CleanupExpired(ctx context.Context) (int64, error) {
	result, err := s.txManager.GetQuerier(ctx).Exec(ctx, `
		DELETE FROM sys_idempotency WHERE expires_at < $1
	`, time.Now().UTC())

	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

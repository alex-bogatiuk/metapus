// Package postgres provides PostgreSQL infrastructure components.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/klauspost/compress/zstd"

	"metapus/internal/core/id"
	"metapus/internal/core/security"
)

// AuditAction represents the type of audited operation.
type AuditAction string

const (
	AuditActionCreate AuditAction = "create"
	AuditActionUpdate AuditAction = "update"
	AuditActionDelete AuditAction = "delete"
	AuditActionPost   AuditAction = "post"
	AuditActionUnpost AuditAction = "unpost"
)

// CompressionAlgo specifies the compression algorithm used.
type CompressionAlgo string

const (
	CompressionNone CompressionAlgo = "none"
	CompressionZstd CompressionAlgo = "zstd"
	CompressionLZ4  CompressionAlgo = "lz4"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	ID                id.ID           `db:"id"`
	EntityType        string          `db:"entity_type"`
	EntityID          id.ID           `db:"entity_id"`
	Action            AuditAction     `db:"action"`
	UserID            string          `db:"user_id"`
	UserEmail         string          `db:"user_email"`
	Changes           json.RawMessage `db:"changes"`
	ChangesCompressed []byte          `db:"changes_compressed"`
	CompressionAlgo   CompressionAlgo `db:"compression_algo"`
	Metadata          json.RawMessage `db:"metadata"`
	CreatedAt         time.Time       `db:"created_at"`
}

// AuditService provides audit logging functionality.
type AuditService struct {
	txManager  *TxManager
	encoder    *zstd.Encoder
	decoder    *zstd.Decoder
	compressThreshold int // bytes, default 10KB
}

// NewAuditService creates a new audit service.
func NewAuditService(txManager *TxManager) (*AuditService, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("create zstd encoder: %w", err)
	}
	
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("create zstd decoder: %w", err)
	}
	
	return &AuditService{
		txManager:         txManager,
		encoder:           encoder,
		decoder:           decoder,
		compressThreshold: 10 * 1024, // 10KB
	}, nil
}

// Log records an audit entry.
func (s *AuditService) Log(ctx context.Context, entry AuditEntry) error {
	// Extract user info from context
	if scope := security.GetScope(ctx); scope != nil {
		if entry.UserID == "" {
			entry.UserID = scope.UserID
		}
	}
	
	// Generate ID if not set
	if id.IsNil(entry.ID) {
		entry.ID = id.New()
	}
	
	// Set timestamp
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	
	// Compress large changes
	entry.CompressionAlgo = CompressionNone
	if len(entry.Changes) > s.compressThreshold {
		compressed := s.encoder.EncodeAll(entry.Changes, nil)
		entry.ChangesCompressed = compressed
		entry.Changes = nil
		entry.CompressionAlgo = CompressionZstd
	}
	
	// Insert
	sql := `
		INSERT INTO sys_audit (
			id, entity_type, entity_id, action, user_id, user_email,
			changes, changes_compressed, compression_algo, metadata, 
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	
	querier := s.txManager.GetQuerier(ctx)
	_, err := querier.Exec(ctx, sql,
		entry.ID, entry.EntityType, entry.EntityID, entry.Action,
		entry.UserID, entry.UserEmail,
		entry.Changes, entry.ChangesCompressed, entry.CompressionAlgo,
		entry.Metadata, entry.CreatedAt,
	)
	
	return err
}

// LogChange is a convenience method for logging entity changes.
func (s *AuditService) LogChange(
	ctx context.Context,
	entityType string,
	entityID id.ID,
	action AuditAction,
	changes map[string]any,
) error {
	changesJSON, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("marshal changes: %w", err)
	}
	
	return s.Log(ctx, AuditEntry{
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		Changes:    changesJSON,
	})
}

// GetEntityHistory retrieves audit history for an entity.
func (s *AuditService) GetEntityHistory(
	ctx context.Context,
	entityType string,
	entityID id.ID,
	limit int,
) ([]AuditEntry, error) {
	sql := `
		SELECT id, entity_type, entity_id, action, user_id, user_email,
			   changes, changes_compressed, compression_algo, metadata,
			   created_at
		FROM sys_audit
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`
	
	rows, err := s.txManager.GetQuerier(ctx).Query(ctx, sql, entityType, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()
	
	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		err := rows.Scan(
			&e.ID, &e.EntityType, &e.EntityID, &e.Action, &e.UserID, &e.UserEmail,
			&e.Changes, &e.ChangesCompressed, &e.CompressionAlgo, &e.Metadata,
			&e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		
		// Decompress if needed
		if e.CompressionAlgo == CompressionZstd && len(e.ChangesCompressed) > 0 {
			decompressed, err := s.decoder.DecodeAll(e.ChangesCompressed, nil)
			if err != nil {
				return nil, fmt.Errorf("decompress changes: %w", err)
			}
			e.Changes = decompressed
			e.ChangesCompressed = nil
		}
		
		entries = append(entries, e)
	}
	
	return entries, rows.Err()
}

// Diff calculates the difference between old and new entity states.
func Diff(oldState, newState map[string]any) map[string]any {
	changes := make(map[string]any)
	
	// Find changed and new fields
	for key, newVal := range newState {
		oldVal, exists := oldState[key]
		if !exists {
			changes[key] = map[string]any{"old": nil, "new": newVal}
		} else if !equal(oldVal, newVal) {
			changes[key] = map[string]any{"old": oldVal, "new": newVal}
		}
	}
	
	// Find deleted fields
	for key, oldVal := range oldState {
		if _, exists := newState[key]; !exists {
			changes[key] = map[string]any{"old": oldVal, "new": nil}
		}
	}
	
	return changes
}

// equal compares two values for equality.
func equal(a, b any) bool {
	// Simple comparison - for production, use reflect.DeepEqual or similar
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

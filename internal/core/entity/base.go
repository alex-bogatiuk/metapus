package entity

import (
	"context"
	"time"

	"metapus/internal/core/id"
)

// Validatable is implemented by entities that support self-validation.
// Validation checks internal invariants (without database access).
type Validatable interface {
	// Validate checks entity invariants.
	// Returns nil if valid, AppError with details otherwise.
	Validate(ctx context.Context) error
}

///////////////////
// Base Entity   //
///////////////////

// BaseEntity contains common fields for all entities (Catalogs, Documents, etc.).
// This is the unified base that eliminates code duplication.
type BaseEntity struct {
	// ID is the primary key (UUIDv7)
	ID id.ID `db:"id" json:"id"`

	// DeletionMark indicates soft-deleted entity
	DeletionMark bool `db:"deletion_mark" json:"deletionMark"`

	// Version for optimistic locking (incremented on each update)
	Version int `db:"version" json:"version"`

	// Attributes stores custom fields (JSONB in PostgreSQL)
	Attributes Attributes `db:"attributes" json:"attributes,omitempty"`

	// CDCFields contains Change Data Capture system fields (_txid, _deleted_at)
	CDCFields
}

// NewBaseEntity creates a new BaseEntity with generated ID.
func NewBaseEntity() BaseEntity {
	return BaseEntity{
		ID:      id.New(),
		Version: 1,
	}
}

// Touch increments version (for optimistic locking).
func (b *BaseEntity) Touch() {
	b.Version++
}

// MarkDeleted sets the deletion mark.
func (b *BaseEntity) MarkDeleted() {
	b.DeletionMark = true
}

// Undelete clears the deletion mark.
func (b *BaseEntity) Undelete() {
	b.DeletionMark = false
}

// SetVersion updates the version number (used by repository after sync).
func (b *BaseEntity) SetVersion(v int) {
	b.Version = v
}

// SetAttribute is a convenience method for setting custom fields.
func (b *BaseEntity) SetAttribute(key string, value any) {
	if b.Attributes == nil {
		b.Attributes = make(Attributes)
	}
	b.Attributes[key] = value
}

// GetAttribute is a convenience method for getting custom fields.
func (b *BaseEntity) GetAttribute(key string) any {
	if b.Attributes == nil {
		return nil
	}
	return b.Attributes[key]
}

///////////////
// Documents //
///////////////

// BaseDocument extends BaseEntity with audit fields for documents.
type BaseDocument struct {
	BaseEntity

	// Audit fields
	CreatedAt time.Time `db:"created_at" json:"createdAt"`
	UpdatedAt time.Time `db:"updated_at" json:"updatedAt"`
	CreatedBy string    `db:"created_by" json:"createdBy,omitempty"`
	UpdatedBy string    `db:"updated_by" json:"updatedBy,omitempty"`
}

// NewBaseDocument creates a new BaseDocument with generated ID and timestamps.
func NewBaseDocument() BaseDocument {
	now := time.Now().UTC()
	return BaseDocument{
		BaseEntity: NewBaseEntity(),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// Touch updates the UpdatedAt timestamp and increments version.
func (b *BaseDocument) Touch() {
	b.UpdatedAt = time.Now().UTC()
	b.BaseEntity.Touch()
}

// SetUpdatedAt updates the updated_at timestamp (used by repository).
func (b *BaseDocument) SetUpdatedAt(t time.Time) {
	b.UpdatedAt = t
}

//////////////
// Catalogs //
//////////////

// BaseCatalog uses BaseEntity directly (no audit fields for catalogs).
type BaseCatalog struct {
	BaseEntity
}

// NewBaseCatalog creates a new BaseCatalog with generated ID.
func NewBaseCatalog() BaseCatalog {
	return BaseCatalog{
		BaseEntity: NewBaseEntity(),
	}
}

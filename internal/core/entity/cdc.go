// Package entity provides core domain entities.
package entity

import "time"

// CDCFields contains fields for Change Data Capture support.
// Embed in entities that need CDC tracking.
type CDCFields struct {
	// DeletedAt is set when entity is soft-deleted
	// Enables DELETE event reconstruction in logical replication
	DeletedAt *time.Time `db:"_deleted_at" json:"-"`
	
	// TxID is the PostgreSQL transaction ID
	// Used for ordering changes in CDC pipelines (more reliable than xmin)
	TxID int64 `db:"_txid" json:"-"`
}

// IsDeleted returns true if entity has been soft-deleted.
func (c *CDCFields) IsDeleted() bool {
	return c.DeletedAt != nil
}

// MarkCDCDeleted sets the deletion timestamp.
func (c *CDCFields) MarkCDCDeleted() {
	now := time.Now().UTC()
	c.DeletedAt = &now
}

// ClearCDCDeleted removes the deletion timestamp (for undelete).
func (c *CDCFields) ClearCDCDeleted() {
	c.DeletedAt = nil
}

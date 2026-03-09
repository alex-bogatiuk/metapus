// Package id provides UUIDv7 generation for all platform entities.
// UUIDv7 is time-ordered, allowing natural sorting by creation time.
package id

import (
	"github.com/google/uuid"
)

// ID is a type alias for UUID, used across all entities.
type ID = uuid.UUID

// New generates a new UUIDv7 (time-ordered UUID).
// UUIDv7 embeds Unix timestamp in first 48 bits, enabling:
// - Natural chronological ordering
// - No need for separate created_at index for sorting
// - Better B-tree locality in PostgreSQL
func New() ID {
	// uuid.NewV7() returns UUIDv7 per RFC 9562
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to V4 if V7 fails (should never happen)
		return uuid.New()
	}
	return id
}

// Parse converts string to ID with validation.
func Parse(s string) (ID, error) {
	return uuid.Parse(s)
}

// MustParse converts string to ID, panics on error.
// Use only for constants and tests.
func MustParse(s string) ID {
	return uuid.MustParse(s)
}

// Nil returns zero-value UUID.
func Nil() ID {
	return uuid.Nil
}

// IsNil checks if ID is zero-value.
func IsNil(id ID) bool {
	return id == uuid.Nil
}

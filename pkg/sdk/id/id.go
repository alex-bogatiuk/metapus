// Package id is the identifier of every record: a UUIDv7.
package id

import core "metapus/internal/core/id"

// ID identifies a record.
type ID = core.ID

// New returns a new UUIDv7.
func New() ID { return core.New() }

// Parse parses a UUID.
func Parse(s string) (ID, error) { return core.Parse(s) }

// Nil is the zero ID.
func Nil() ID { return core.Nil() }

// IsNil reports whether v is the zero ID.
func IsNil(v ID) bool { return core.IsNil(v) }

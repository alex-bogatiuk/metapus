// Package id is the identifier of every record: a UUIDv7.
package id

import (
	core "metapus/internal/core/id"
)

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

// Compact returns the complete UUID as 32 lowercase hexadecimal characters.
// Unlike prefixes of UUIDv7 strings, it preserves both the timestamp and all
// random bits and is therefore safe for deriving externally visible IDs.
func Compact(value core.ID) string { return core.Compact(value) }

// MustParse converts string to ID, panics on error.
// Use only for constants and tests.
func MustParse(s string) core.ID { return core.MustParse(s) }

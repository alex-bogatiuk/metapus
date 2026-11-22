// Package cursor is keyset pagination of lists.
package cursor

import (
	core "metapus/internal/domain/cursor"
)

type (
	// Request contains cursor pagination parameters parsed from query string.
	Request = core.Request
)

const (
	DirAfter  = core.DirAfter
	DirAround = core.DirAround
	DirBefore = core.DirBefore
)

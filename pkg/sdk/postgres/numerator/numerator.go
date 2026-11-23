// Package numerator stores number series in PostgreSQL.
package numerator

import (
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/numerator"
)

// New creates a new numerator service.
// Uses Database-per-Tenant architecture: tenant pool from context.
func New() *core.Service { return core.New() }

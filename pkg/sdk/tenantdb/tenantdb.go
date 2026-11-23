// Package tenantdb is the manager of tenant databases. A route that finds
// its tenant itself (a merchant API key, a checkout link) hands it to
// middleware.BindTenant.
package tenantdb

import (
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/tenantdb"
)

// Manager manages database connections for multiple tenants.
// Thread-safe for concurrent access.
type Manager = core.Manager

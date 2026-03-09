package tenant

import "errors"

var (
	// ErrTenantNotFound is returned when tenant does not exist in meta-database.
	ErrTenantNotFound = errors.New("tenant not found")

	// ErrTenantNotActive is returned when tenant exists but is not active.
	ErrTenantNotActive = errors.New("tenant is not active")

	// ErrMaxPoolLimit is returned when tenant manager reached pool limit.
	ErrMaxPoolLimit = errors.New("max tenant pool limit reached")
)


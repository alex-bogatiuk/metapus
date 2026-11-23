package security_profile

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

// DimensionResolver resolves a dimension's values from runtime data when the
// security context is built.
//
// A resolved value replaces the security profile's value for that dimension,
// it does not narrow it. So a resolver must not handle a dimension that
// administrators set in security profiles: a profile limited to merchant A
// would read whatever the resolver returns. Row restrictions of ERP reads and
// reports come from the profile alone, the same for HTTP and scheduled runs.
type DimensionResolver interface {
	// DimensionName returns the dimension name this resolver handles.
	DimensionName() string

	// Resolve returns the allowed IDs for the given user.
	// Returns nil (not empty slice) if dimension does not apply to this user.
	Resolve(ctx context.Context, userID id.ID) ([]string, error)
}

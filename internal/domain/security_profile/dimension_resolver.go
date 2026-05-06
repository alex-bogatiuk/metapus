package security_profile

import (
	"context"

	"metapus/internal/core/id"
)

// DimensionResolver dynamically resolves dimension values at request time.
// Used for dimensions that depend on runtime data (e.g., UserID → MerchantIDs).
//
// Static dimensions (from SecurityProfile DB rows) are loaded once and cached.
// Dynamic dimensions are resolved per-request and merged into DataScope.
type DimensionResolver interface {
	// DimensionName returns the dimension name this resolver handles (e.g., "merchant").
	DimensionName() string

	// Resolve returns the allowed IDs for the given user.
	// Returns nil (not empty slice) if dimension does not apply to this user.
	Resolve(ctx context.Context, userID id.ID) ([]string, error)
}

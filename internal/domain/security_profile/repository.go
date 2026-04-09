package security_profile

import (
	"context"

	"metapus/internal/core/id"
)

// PolicyRuleRepository defines persistence operations for CEL policy rules.
type PolicyRuleRepository interface {
	// Create inserts a new policy rule.
	Create(ctx context.Context, rule *PolicyRule) error

	// GetByID retrieves a policy rule by ID.
	GetByID(ctx context.Context, ruleID id.ID) (*PolicyRule, error)

	// Update modifies an existing policy rule.
	Update(ctx context.Context, rule *PolicyRule) error

	// Delete removes a policy rule.
	Delete(ctx context.Context, ruleID id.ID) error

	// ListByProfileID returns all rules for a profile, ordered by priority DESC.
	ListByProfileID(ctx context.Context, profileID id.ID) ([]*PolicyRule, error)
}

// Repository defines persistence operations for SecurityProfile.
type Repository interface {
	// GetByID retrieves a single profile by ID (with dimensions + field policies).
	GetByID(ctx context.Context, profileID id.ID) (*SecurityProfile, error)

	// GetByUserID loads the effective security profile for a user.
	// If the user has multiple profiles, dimensions and field policies are merged.
	// Returns nil (no error) if the user has no assigned profile.
	GetByUserID(ctx context.Context, userID id.ID) (*SecurityProfile, error)

	// List returns all profiles (admin panel).
	List(ctx context.Context) ([]*SecurityProfile, error)

	// Create inserts a new profile with its dimensions and field policies.
	Create(ctx context.Context, profile *SecurityProfile) error

	// Update modifies an existing profile (replaces dimensions + field policies).
	Update(ctx context.Context, profile *SecurityProfile) error

	// Delete removes a profile (cascade deletes dimensions, field policies, user mappings).
	Delete(ctx context.Context, profileID id.ID) error

	// AssignToUser links a user to a security profile.
	AssignToUser(ctx context.Context, userID, profileID id.ID) error

	// RemoveFromUser unlinks a user from a security profile.
	RemoveFromUser(ctx context.Context, userID, profileID id.ID) error
}

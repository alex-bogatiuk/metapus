// Package security_profile is the security profile model.
package security_profile

import (
	core "metapus/internal/domain/security_profile"
)

type (
	// PolicyRule is a CEL-based authorization rule attached to a SecurityProfile.
	// When evaluated, the CEL expression receives document/entity data, user context,
	// action name, and current timestamp. The expression must return a bool.
	//
	// Semantics:
	//   - effect="deny":  if expression evaluates to true → operation is DENIED
	//   - effect="allow": if expression evaluates to true → operation is ALLOWED
	//
	// Rules are evaluated in priority order (descending). First matching rule wins.
	// If no rule matches → operation is allowed (RLS/FLS already filtered).
	PolicyRule = core.PolicyRule
)

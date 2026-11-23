package readpolicy

import (
	"context"
	"github.com/alex-bogatiuk/metapus/internal/application/readaccess"

	"github.com/Masterminds/squirrel"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
)

// SQLRegistry supplies trusted projections equivalent to the policy engine's
// entity codec. Unsupported types must be absent, never approximated by a cast.
type SQLRegistry interface {
	PolicyProjection(key, alias string) (map[string]SQLPolicyField, error)
}

// SQLConstraints performs no reads. Call it for every source before executing
// any part of an aggregate query, including sources used only in joins.
func SQLConstraints(ctx context.Context, s *readaccess.Service, key, alias string) (squirrel.Sqlizer, error) {
	d, err := s.Require(ctx, key)
	if err != nil {
		return nil, err
	}
	dimensions := make(map[string]string, len(d.Dimensions))
	for dimension, column := range d.Dimensions {
		dimensions[dimension] = alias + "." + column
	}
	conditions := squirrel.And(ApplyConditions(security.GetDataScope(ctx), d.Key, dimensions))
	if d.SoftDelete {
		conditions = append(conditions, squirrel.Eq{alias + "._deleted_at": nil})
	}
	rules := security.GetApplicableRules(ctx, d.Key, "read")
	if len(rules) == 0 {
		return conditions, nil
	}
	projection, ok := s.Definitions().(SQLRegistry)
	if !ok || s.PolicyEngine() == nil {
		err := apperror.NewForbidden("report policy compiler is unavailable")
		err.Code = "REPORT_POLICY_UNSUPPORTED"
		return nil, err
	}
	fields, err := projection.PolicyProjection(d.Key, alias)
	if err != nil {
		return nil, err
	}
	predicate, err := CompileReadSQL(ctx, s.PolicyEngine(), rules, fields)
	if err != nil {
		return nil, err
	}
	return append(conditions, predicate), nil
}

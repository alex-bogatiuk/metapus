package readpolicy

import (
	"context"
	"fmt"
	"github.com/Masterminds/squirrel"
	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"metapus/internal/domain/reports/schema"
)

// AuthorizeReport builds sources before the executor can add SUM, UNION, or
// grouping. No SQL is executed until all source and reference policies compile.
func AuthorizeReport(ctx context.Context, s *readaccess.Service, ds *schema.Dataset) (ReadConstraints, error) {
	plan := ReadConstraints{Sources: make(map[string]squirrel.SelectBuilder)}
	if err := s.RequireReport(ctx, ds); err != nil {
		return plan, err
	}
	if len(ds.Sources) == 0 {
		return plan, apperror.NewForbidden("report sources are not registered")
	}
	if err := s.RequireReportFields(ctx, ds.ImplicitAccess); err != nil {
		return plan, err
	}
	for _, source := range ds.Sources {
		definition, err := s.Require(ctx, source.Entity)
		if err != nil {
			return plan, err
		}
		constraints, err := SQLConstraints(ctx, s, source.Entity, source.Alias)
		if err != nil {
			return plan, err
		}
		q := squirrel.Select().From(definition.Table + " AS " + source.Alias).Where(constraints)
		for _, relation := range source.Relations {
			related, err := s.Require(ctx, relation.Entity)
			if err != nil {
				return plan, err
			}
			predicate, err := SQLConstraints(ctx, s, relation.Entity, relation.Alias)
			if err != nil {
				return plan, err
			}
			join := fmt.Sprintf("%s AS %s ON %s.%s = %s.id AND ?", related.Table, relation.Alias, source.Alias, relation.Column, relation.Alias)
			prefix := "LEFT JOIN "
			if relation.Required {
				prefix = "JOIN "
			}
			q = q.JoinClause(squirrel.Expr(prefix+join, predicate))
		}
		if _, duplicate := plan.Sources[source.Key]; duplicate {
			return plan, apperror.NewInternal(fmt.Errorf("duplicate report source"))
		}
		plan.Sources[source.Key] = q
	}
	return plan, nil
}

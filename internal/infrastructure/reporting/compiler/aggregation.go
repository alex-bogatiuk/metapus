package compiler

import (
	"fmt"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
	"strings"
)

type queryGrouping struct {
	resolver    *resolver
	enabled     bool
	paths       map[string]bool
	expressions []string
	seen        map[string]bool
}

func newQueryGrouping(r *resolver, groups, selected []string) (*queryGrouping, error) {
	g := &queryGrouping{resolver: r, enabled: len(groups) > 0, paths: map[string]bool{}, seen: map[string]bool{}}
	if !g.enabled {
		return g, nil
	}
	for _, path := range groups {
		path = strings.ReplaceAll(path, "__", ".")
		expr, err := r.ResolveForGroupBy(path)
		if err != nil {
			return nil, reportPathError("invalid report grouping", path, err)
		}
		g.paths[path] = true
		g.add(expr)
	}
	// Aggregating a monetary measure always partitions by its declared unit.
	for _, path := range selected {
		field := r.dataset.FindField(path)
		if field == nil || field.Type != schema.TypeMoney || field.Money == nil {
			continue
		}
		for _, unit := range []string{field.Money.CurrencyID, field.Money.CurrencyCode, field.Money.DecimalPlaces} {
			expr, err := r.ResolveForGroupBy(unit)
			if err != nil {
				return nil, err
			}
			g.paths[unit] = true
			g.add(expr)
		}
	}
	return g, nil
}

func (g *queryGrouping) add(expr string) {
	if !g.seen[expr] {
		g.expressions = append(g.expressions, expr)
		g.seen[expr] = true
	}
}

func (g *queryGrouping) expression(path string, selectAlias bool) (string, error) {
	expr, alias, err := g.resolver.resolvePath(path, true)
	if err != nil {
		return "", err
	}
	if g.enabled && !g.paths[path] {
		field := g.resolver.dataset.FindField(path)
		if field != nil && field.Kind == schema.FieldMeasure {
			switch field.Agg {
			case schema.AggSum, schema.AggMin, schema.AggMax, schema.AggCount, schema.AggAvg:
				expr = strings.ToUpper(string(field.Agg)) + "(" + expr + ")"
				if field.Type == schema.TypeMoney && field.Agg == schema.AggAvg {
					expr = "ROUND(" + expr + ")"
				}
			default:
				return "", apperror.NewValidation("report measure has no supported aggregation").WithDetail("field", path)
			}
		} else {
			// A reference presentation is functionally dependent on the grouped UUID.
			// Include it explicitly so PostgreSQL can validate the SELECT expression.
			root, _, child := strings.Cut(path, ".")
			rootField := g.resolver.dataset.FindField(root)
			if !child || !g.paths[root] || rootField == nil || rootField.Type != schema.TypeRef {
				return "", apperror.NewValidation("selected or sorted field must be grouped or aggregated").WithDetail("field", path)
			}
			g.add(expr)
		}
	}
	if selectAlias {
		return fmt.Sprintf(`%s AS "%s"`, expr, alias), nil
	}
	return expr, nil
}

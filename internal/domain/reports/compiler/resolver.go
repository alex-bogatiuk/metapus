package compiler

import (
	"fmt"
	"strings"

	"metapus/internal/domain/reports/schema"
	"metapus/internal/metadata"
)

// JoinStep describes a single LEFT JOIN needed to resolve a reference field path.
type JoinStep struct {
	// Alias is the unique alias for this join, e.g. "j1", "j2".
	Alias string
	// Table is the target table name, e.g. "cat_warehouses".
	Table string
	// JoinKey is the FK column in the parent, e.g. "warehouse_id".
	JoinKey string
	// ParentAlias is the alias of the parent table, e.g. "base" or "j1".
	ParentAlias string
}

// resolver tracks JOIN deduplication and field path validation.
// Created per-request — not safe for concurrent use.
type resolver struct {
	registry *metadata.Registry
	dataset  *schema.Dataset
	maxDepth int

	// Deduplicated joins in order of discovery.
	joins []JoinStep
	// joinIndex maps "parentAlias.joinKey" → alias for deduplication.
	joinIndex map[string]string
	// Counter for generating unique aliases.
	aliasCounter int
}

// newResolver creates a resolver for a single query compilation.
func newResolver(reg *metadata.Registry, ds *schema.Dataset, maxDepth int) *resolver {
	return &resolver{
		registry:  reg,
		dataset:   ds,
		maxDepth:  maxDepth,
		joins:     make([]JoinStep, 0),
		joinIndex: make(map[string]string),
	}
}

// Joins returns all collected JoinSteps (deduplicated, in discovery order).
func (r *resolver) Joins() []JoinStep {
	return r.joins
}

// Resolve resolves a field path like "nomenclature_id.brand_id.name" into a SQL expression.
//
// Returns the fully qualified SQL expression, e.g.:
//   - "base.quantity"              (simple field)
//   - "base.warehouse_id"         (ref field, no dereference)
//   - "j1.name"                   (dereferenced: warehouse_id.name)
//   - "j2.name"                   (deeply dereferenced: nomenclature_id.brand_id.name)
//
// Side effect: registers necessary JoinSteps for referenced tables.
func (r *resolver) Resolve(path string) (string, error) {
	expr, _, err := r.resolvePath(path, false)
	return expr, err
}

// ResolveForGroupBy resolves a field path for GROUP BY clause.
// Returns the raw column expression without alias.
func (r *resolver) ResolveForGroupBy(path string) (string, error) {
	expr, _, err := r.resolvePath(path, true)
	return expr, err
}

// ResolveForOrderBy resolves a field path for ORDER BY clause.
func (r *resolver) ResolveForOrderBy(path string) (string, error) {
	expr, _, err := r.resolvePath(path, true)
	return expr, err
}

// ResolveForWhere resolves a field path for WHERE clause.
// Returns the bare SQL expression (no alias) and registers necessary JOINs.
// Used by applyAdvancedFilters to compile FilterSidebar conditions into SQL.
func (r *resolver) ResolveForWhere(path string) (string, error) {
	expr, _, err := r.resolvePath(path, true)
	return expr, err
}

// resolvePath does the actual path resolution.
// If bare=true, returns without AS alias (for GROUP BY / ORDER BY).
func (r *resolver) resolvePath(path string, bare bool) (string, string, error) {
	segments := strings.Split(path, ".")

	// Single-segment: direct field from dataset
	if len(segments) == 1 {
		field := r.dataset.FindField(segments[0])
		if field == nil {
			return "", "", fmt.Errorf("field %q not found in dataset %q", segments[0], r.dataset.Key)
		}
		expr := "base." + field.Name
		if !bare && field.Alias != "" {
			expr += " AS \"" + field.Alias + "\""
		}
		return expr, field.OutputName(), nil
	}

	// Multi-segment: walk through reference chain
	if len(segments)-1 > r.maxDepth {
		return "", "", fmt.Errorf("path %q exceeds max depth %d", path, r.maxDepth)
	}

	currentAlias := "base"
	var currentEntityDef *metadata.EntityDef

	// Walk all segments except the last one (those are ref fields)
	for i := 0; i < len(segments)-1; i++ {
		segName := segments[i]

		// First segment: find in dataset fields
		if i == 0 {
			field := r.dataset.FindField(segName)
			if field == nil {
				return "", "", fmt.Errorf("field %q not found in dataset %q", segName, r.dataset.Key)
			}
			if field.Type != schema.TypeRef || field.RefEntity == "" {
				return "", "", fmt.Errorf("field %q is not a reference (type=%s)", segName, field.Type)
			}

			// Resolve the entity from registry
			entityName, ok := r.registry.GetEntityByRefType(field.RefEntity)
			if !ok {
				// Fallback: try direct entity lookup by RefEntity key
				def, found := r.registry.Get(field.RefEntity)
				if !found {
					return "", "", fmt.Errorf("entity %q not found in registry for field %q", field.RefEntity, segName)
				}
				currentEntityDef = &def
			} else {
				def, found := r.registry.Get(entityName)
				if !found {
					return "", "", fmt.Errorf("entity %q not found in registry", entityName)
				}
				currentEntityDef = &def
			}

			// Register JOIN
			joinAlias := r.ensureJoin(currentAlias, segName, r.entityTable(currentEntityDef))
			currentAlias = joinAlias
		} else {
			// Subsequent segments: find in the resolved entity's fields
			if currentEntityDef == nil {
				return "", "", fmt.Errorf("cannot resolve segment %q: no entity context", segName)
			}

			var refField *metadata.FieldDef
			for j := range currentEntityDef.Fields {
				if currentEntityDef.Fields[j].Name == segName {
					refField = &currentEntityDef.Fields[j]
					break
				}
			}
			if refField == nil {
				return "", "", fmt.Errorf("field %q not found in entity %q", segName, currentEntityDef.Name)
			}
			if refField.Type != metadata.TypeReference || refField.ReferenceType == "" {
				return "", "", fmt.Errorf("field %q in entity %q is not a reference", segName, currentEntityDef.Name)
			}

			// Resolve next entity
			entityName, ok := r.registry.GetEntityByRefType(refField.ReferenceType)
			if !ok {
				return "", "", fmt.Errorf("entity for ref type %q not found", refField.ReferenceType)
			}
			def, found := r.registry.Get(entityName)
			if !found {
				return "", "", fmt.Errorf("entity %q not found in registry", entityName)
			}
			currentEntityDef = &def

			// Convert camelCase field name to snake_case column name for JOIN
			colName := r.fieldToColumn(refField)
			joinAlias := r.ensureJoin(currentAlias, colName, r.entityTable(currentEntityDef))
			currentAlias = joinAlias
		}
	}

	// Last segment: the actual attribute to select
	lastSeg := segments[len(segments)-1]
	if currentEntityDef == nil {
		return "", "", fmt.Errorf("cannot resolve final segment %q: no entity context", lastSeg)
	}

	// Validate the final field exists in the entity
	var finalField *metadata.FieldDef
	for i := range currentEntityDef.Fields {
		if currentEntityDef.Fields[i].Name == lastSeg {
			finalField = &currentEntityDef.Fields[i]
			break
		}
	}
	if finalField == nil {
		return "", "", fmt.Errorf("field %q not found in entity %q", lastSeg, currentEntityDef.Name)
	}

	// Build the output alias: "nomenclature_id__brand_id__name"
	outputAlias := strings.Join(segments, "__")
	colName := r.fieldToColumn(finalField)
	expr := currentAlias + "." + colName
	if !bare {
		expr += " AS \"" + outputAlias + "\""
	}

	return expr, outputAlias, nil
}

// ensureJoin registers a JOIN if not already present, returns the alias.
func (r *resolver) ensureJoin(parentAlias, joinKey, table string) string {
	dedupKey := parentAlias + "." + joinKey
	if alias, exists := r.joinIndex[dedupKey]; exists {
		return alias
	}

	r.aliasCounter++
	alias := fmt.Sprintf("j%d", r.aliasCounter)

	r.joins = append(r.joins, JoinStep{
		Alias:       alias,
		Table:       table,
		JoinKey:     joinKey,
		ParentAlias: parentAlias,
	})
	r.joinIndex[dedupKey] = alias
	return alias
}

// entityTable derives the SQL table name from EntityDef.
// Uses TableName if set, otherwise derives from entity key.
func (r *resolver) entityTable(def *metadata.EntityDef) string {
	if def.TableName != "" {
		return def.TableName
	}
	// Convention: catalog entities → "cat_{routePrefix}", documents → "doc_{routePrefix}"
	if def.RoutePrefix != "" {
		switch def.Type {
		case metadata.TypeCatalog:
			return "cat_" + def.RoutePrefix
		case metadata.TypeDocument:
			return "doc_" + def.RoutePrefix
		}
	}
	// Fallback: use key with prefix
	return "cat_" + def.Key
}

// fieldToColumn converts a metadata FieldDef name (camelCase JSON) to DB column (snake_case).
// E.g. "brandId" → "brand_id".
func (r *resolver) fieldToColumn(f *metadata.FieldDef) string {
	// metadata.FieldDef.Name is the JSON name (camelCase).
	// We need the DB column name. Use a simple camelCase → snake_case converter.
	return camelToSnake(f.Name)
}

// camelToSnake converts camelCase to snake_case.
// E.g. "brandId" → "brand_id", "baseUnitId" → "base_unit_id".
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(r + 32) // toLower
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

package postgres

import (
	"context"
	"fmt"
	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"metapus/internal/core/security"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/metadata"
)

// RefFinderRepo scans all registered entity tables for references to a given object.
// Uses metadata.Registry to discover TypedRef and FK reference fields,
// then queries each relevant table dynamically.
//
// Analogous to 1C's "Найти ссылки на объект" processing.
type RefFinderRepo struct {
	registry *metadata.Registry
}

// NewRefFinderRepo creates a new RefFinderRepo.
func NewRefFinderRepo(registry *metadata.Registry) *RefFinderRepo {
	return &RefFinderRepo{registry: registry}
}

// refFieldSpec describes a field in a table that can reference other entities.
type refFieldSpec struct {
	tableName   string // SQL table name
	fieldName   string // JSON field name for display
	isTypedRef  bool   // true = TypedRef (ref_type + ref_id), false = FK (xxx_id)
	dbColumn    string // DB column name for FK references (e.g. "counterparty_id")
	isTablePart bool   // true when field lives in a lines table (PK = line_id, parent = document_id)
	entityDef   metadata.EntityDef
}

// FindReferences searches all registered entity tables for references to the target.
func (r *RefFinderRepo) FindReferences(ctx context.Context, req domain.FindReferencesRequest) ([]domain.FoundReference, error) {
	specs := r.buildRefSpecs(req.EntityName)
	reads, ok := readaccess.FromContext(ctx)
	if !ok {
		return nil, apperror.NewForbidden("authorized reference reader is unavailable")
	}
	visibleSpecs := make([]refFieldSpec, 0, len(specs))
	for _, spec := range specs {
		if _, err := reads.Require(ctx, spec.entityDef.Key); err != nil {
			if apperror.GetHTTPStatus(err) == http.StatusForbidden {
				continue
			}
			return nil, err
		}
		if policy := security.GetFieldPolicy(ctx, spec.entityDef.Key, "read"); policy != nil {
			fields := []string{spec.dbColumn}
			if spec.isTypedRef {
				fields = []string{"basis_type", "basis_id"}
			}
			allowed := true
			for _, field := range fields {
				if part, _, found := strings.Cut(spec.fieldName, "."); found {
					allowed = allowed && policy.IsFieldAllowed(part) && policy.IsTablePartFieldAllowed(part, field)
				} else {
					allowed = allowed && policy.IsFieldAllowed(field)
				}
			}
			if !allowed {
				continue
			}
		}
		visibleSpecs = append(visibleSpecs, spec)
	}
	specs = visibleSpecs
	if len(specs) == 0 {
		return nil, nil
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	resolver := NewRefResolverRepo(r.registry)
	results := make([]domain.FoundReference, 0, len(specs))
	resolveReqs := make([]domain.RefResolveRequest, 0, len(specs))

	b := &pgx.Batch{}
	for _, spec := range specs {
		sql, args := r.buildFindQuery(spec, req)
		b.Queue(sql, args...)
	}

	br := querier.SendBatch(ctx, b)
	defer func() {
		_ = br.Close()
	}()

	for i := range specs {
		spec := specs[i]
		rows, err := br.Query()
		if err != nil {
			return nil, fmt.Errorf("find references in %s: %w", spec.tableName, err)
		}

		for rows.Next() {
			var sourceID id.ID
			if err := rows.Scan(&sourceID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("find references in %s: %w", spec.tableName, err)
			}
			ref := domain.FoundReference{
				SourceEntityName: spec.entityDef.Name,
				SourceEntityType: string(spec.entityDef.Type),
				SourceField:      spec.fieldName,
				SourceID:         sourceID,
			}
			results = append(results, ref)
			resolveReqs = append(resolveReqs, domain.RefResolveRequest{RefType: spec.entityDef.Name, RefID: sourceID})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("find references in %s: %w", spec.tableName, err)
		}
	}

	if err := br.Close(); err != nil {
		return nil, err
	}
	if len(resolveReqs) == 0 {
		return results, nil
	}
	// Marked objects still reference the target and block its deletion, so
	// they are listed too, flagged by their deletion mark.
	resolved, err := resolver.ResolveRefsWithDeletionMarked(ctx, resolveReqs)
	if err != nil {
		return nil, err
	}
	presentations := make(map[domain.RefResolveRequest]domain.RefResolveResult, len(resolved))
	for _, row := range resolved {
		presentations[domain.RefResolveRequest{RefType: row.RefType, RefID: row.RefID}] = row
	}
	allowed := make([]domain.FoundReference, 0, len(results))
	for _, row := range results {
		if source, ok := presentations[domain.RefResolveRequest{RefType: row.SourceEntityName, RefID: row.SourceID}]; ok {
			row.Presentation = source.Presentation
			row.DeletionMark = source.DeletionMark
			row.Posted = source.Posted
			allowed = append(allowed, row)
		}
	}
	results = allowed

	return results, nil
}

// CountReferences returns total count of references (fast path for delete check).
func (r *RefFinderRepo) CountReferences(ctx context.Context, req domain.FindReferencesRequest) (_ int, retErr error) {
	specs := r.buildRefSpecs(req.EntityName)
	if len(specs) == 0 {
		return 0, nil
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	var total int

	// Batch counts
	b := &pgx.Batch{}
	for _, spec := range specs {
		sql, args := r.buildCountQuery(spec, req)
		b.Queue(sql, args...)
	}

	br := querier.SendBatch(ctx, b)
	defer func() {
		if cErr := br.Close(); cErr != nil && retErr == nil {
			retErr = cErr
		}
	}()

	// A failed count must not let a referenced object be deleted.
	for _, spec := range specs {
		var count int
		if err := br.QueryRow().Scan(&count); err != nil {
			return 0, fmt.Errorf("count references in %s: %w", spec.tableName, err)
		}
		total += count
	}

	return total, nil
}

// CountReferencesBatch returns counts of references for multiple objects of the same entity type.
func (r *RefFinderRepo) CountReferencesBatch(ctx context.Context, targetEntityName string, targetIDs []id.ID) (_ map[id.ID]int, retErr error) {
	result := make(map[id.ID]int)
	if len(targetIDs) == 0 {
		return result, nil
	}
	specs := r.buildRefSpecs(targetEntityName)
	if len(specs) == 0 {
		return result, nil
	}

	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	b := &pgx.Batch{}

	for _, spec := range specs {
		var sql, countExpr, groupCol string
		if spec.isTypedRef {
			groupCol = "ref_id"
			if spec.isTablePart {
				countExpr = "COUNT(DISTINCT document_id)"
			} else {
				countExpr = "COUNT(*)"
			}
			sql = fmt.Sprintf(`SELECT %s, %s FROM %s WHERE ref_type = $1 AND ref_id = ANY($2) GROUP BY %s`, groupCol, countExpr, spec.tableName, groupCol)
			b.Queue(sql, targetEntityName, targetIDs)
		} else {
			groupCol = spec.dbColumn
			if spec.isTablePart {
				countExpr = "COUNT(DISTINCT document_id)"
			} else {
				countExpr = "COUNT(*)"
			}
			sql = fmt.Sprintf(`SELECT %s, %s FROM %s WHERE %s = ANY($1) GROUP BY %s`, groupCol, countExpr, spec.tableName, groupCol, groupCol)
			b.Queue(sql, targetIDs)
		}
	}

	br := querier.SendBatch(ctx, b)
	defer func() {
		if cErr := br.Close(); cErr != nil && retErr == nil {
			retErr = cErr
		}
	}()

	for _, spec := range specs {
		rows, err := br.Query()
		if err != nil {
			return nil, fmt.Errorf("count references in %s: %w", spec.tableName, err)
		}
		for rows.Next() {
			var targetID id.ID
			var count int
			if err := rows.Scan(&targetID, &count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("count references in %s: %w", spec.tableName, err)
			}
			result[targetID] += count
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("count references in %s: %w", spec.tableName, err)
		}
	}

	return result, nil
}

// buildRefSpecs scans the metadata registry and builds specs for all fields
// that could reference the target entity type.
func (r *RefFinderRepo) buildRefSpecs(targetEntityName string) []refFieldSpec {
	specs := make([]refFieldSpec, 0, 16)

	for _, def := range r.registry.List() {
		tableName := deriveTableName(def)
		if tableName == "" {
			continue
		}

		// Within the target's own table only the hierarchy parent refers to
		// the same entity; other self-typed fields would match by name only.
		if def.Name == targetEntityName {
			for _, field := range def.Fields {
				if field.Type == metadata.TypeReference && field.ReferenceType == "parent" {
					specs = append(specs, refFieldSpec{
						tableName: tableName,
						fieldName: field.Name,
						dbColumn:  referenceColumn(field),
						entityDef: def,
					})
				}
			}
			continue
		}

		// Scan header fields
		for _, field := range def.Fields {
			if spec := r.fieldToSpec(field, tableName, "", def, targetEntityName); spec != nil {
				specs = append(specs, *spec)
			}
		}

		// Only declared SQL parts are scanned; derived collections have no table.
		for _, tp := range def.TableParts {
			if tp.SQLTable == "" {
				continue
			}
			for _, col := range tp.Columns {
				if spec := r.fieldToSpec(col, tp.SQLTable, tp.Name+".", def, targetEntityName); spec != nil {
					spec.isTablePart = true
					specs = append(specs, *spec)
				}
			}
		}
	}

	return specs
}

// fieldToSpec checks if a field can reference the target entity and returns a spec.
func (r *RefFinderRepo) fieldToSpec(
	field metadata.FieldDef,
	tableName, fieldPrefix string,
	entityDef metadata.EntityDef,
	targetEntityName string,
) *refFieldSpec {
	switch field.Type {
	case metadata.TypeTypedRef:
		// TypedRef can reference any type — always include
		return &refFieldSpec{
			tableName:  tableName,
			fieldName:  fieldPrefix + field.Name,
			isTypedRef: true,
			entityDef:  entityDef,
		}

	case metadata.TypeReference:
		// FK reference — check if the reference type matches target
		if field.ReferenceType != "" && r.refTypeMatchesEntity(field.ReferenceType, targetEntityName) {
			return &refFieldSpec{
				tableName:  tableName,
				fieldName:  fieldPrefix + field.Name,
				isTypedRef: false,
				dbColumn:   referenceColumn(field),
				entityDef:  entityDef,
			}
		}
	}

	return nil
}

// refTypeMatchesEntity checks if a reference type maps to the target entity name.
// Handles both single-word ("merchant" ↔ "Merchant" via EqualFold) and
// multi-word ("crypto_invoice" ↔ "CryptoInvoice" via registry Key→Name lookup).
func (r *RefFinderRepo) refTypeMatchesEntity(refType, targetEntityName string) bool {
	// Direct case-insensitive match (single-word entities)
	if strings.EqualFold(refType, targetEntityName) {
		return true
	}
	// Registry lookup: snake_case ref tag → entity Key → entity Name
	for _, def := range r.registry.List() {
		if def.Key == refType && def.Name == targetEntityName {
			return true
		}
	}
	return false
}

// buildFindQuery builds the SQL query for finding references to the target.
func (r *RefFinderRepo) buildFindQuery(spec refFieldSpec, req domain.FindReferencesRequest) (string, []any) {
	var sql string
	var args []any

	// For table parts (lines), the PK is line_id; we need the parent document_id.
	selectCol := "id"
	if spec.isTablePart {
		selectCol = "document_id"
	}

	if spec.isTypedRef {
		sql = fmt.Sprintf(
			`SELECT DISTINCT %s FROM %s WHERE ref_type = $1 AND ref_id = $2 LIMIT 100`,
			selectCol, spec.tableName,
		)
		args = []any{req.EntityName, req.EntityID}
	} else {
		sql = fmt.Sprintf(
			`SELECT DISTINCT %s FROM %s WHERE %s = $1 LIMIT 100`,
			selectCol, spec.tableName, spec.dbColumn,
		)
		args = []any{req.EntityID}
	}

	return sql, args
}

// buildCountQuery builds the SQL query for counting references to the target (fast path).
func (r *RefFinderRepo) buildCountQuery(spec refFieldSpec, req domain.FindReferencesRequest) (string, []any) {
	var sql string
	var args []any

	// For table parts, count distinct parent documents (not individual lines).
	countExpr := "COUNT(*)"
	if spec.isTablePart {
		countExpr = "COUNT(DISTINCT document_id)"
	}

	if spec.isTypedRef {
		sql = fmt.Sprintf(
			`SELECT %s FROM %s WHERE ref_type = $1 AND ref_id = $2`,
			countExpr, spec.tableName,
		)
		args = []any{req.EntityName, req.EntityID}
	} else {
		sql = fmt.Sprintf(
			`SELECT %s FROM %s WHERE %s = $1`,
			countExpr, spec.tableName, spec.dbColumn,
		)
		args = []any{req.EntityID}
	}

	return sql, args
}

// referenceColumn is the column of a reference field: the declared one, or
// the snake_case field name with an "_id" suffix.
func referenceColumn(field metadata.FieldDef) string {
	if field.DBColumn != "" {
		return field.DBColumn
	}
	column := toSnakeCase(field.Name)
	if !strings.HasSuffix(column, "_id") {
		column += "_id"
	}
	return column
}

// toSnakeCase converts camelCase field name to snake_case column name.
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r | 0x20) // lowercase
	}
	return result.String()
}

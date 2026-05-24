package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/metadata"
	"metapus/pkg/logger"
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
			logger.Warn(ctx, "FindReferences skipped table", "table", spec.tableName, "error", err)
			continue
		}

		for rows.Next() {
			var sourceID id.ID
			if err := rows.Scan(&sourceID); err != nil {
				continue
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
	}

	if len(resolveReqs) > 0 {
		resolved, err := resolver.ResolveRefs(ctx, resolveReqs)
		if err == nil {
			presMap := make(map[string]string)
			for _, res := range resolved {
				key := res.RefType + ":" + res.RefID.String()
				presMap[key] = res.Presentation
			}
			for i := range results {
				key := results[i].SourceEntityName + ":" + results[i].SourceID.String()
				if pres, ok := presMap[key]; ok {
					results[i].Presentation = pres
				}
			}
		}
	}

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

	for range specs {
		var count int
		err := br.QueryRow().Scan(&count)
		if err != nil {
			continue // skip errors for individual tables
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

	for range specs {
		rows, err := br.Query()
		if err != nil {
			continue
		}
		for rows.Next() {
			var targetID id.ID
			var count int
			if err := rows.Scan(&targetID, &count); err == nil {
				result[targetID] += count
			}
		}
		rows.Close()
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

		// Skip self-references
		if def.Name == targetEntityName {
			continue
		}

		// Scan header fields
		for _, field := range def.Fields {
			if spec := r.fieldToSpec(field, tableName, "", def, targetEntityName); spec != nil {
				specs = append(specs, *spec)
			}
		}

		// Scan table parts — lines tables follow convention: {table}_lines
		for _, tp := range def.TableParts {
			// Metapus uses singular base name for parts: doc_goods_receipt_lines vs doc_goods_receipts_lines
			var baseName string
			switch def.Type {
			case metadata.TypeCatalog:
				baseName = "cat_" + toSnakeCase(def.Name)
			case metadata.TypeDocument:
				baseName = "doc_" + toSnakeCase(def.Name)
			default:
				baseName = tableName
			}
			linesTable := baseName + "_" + tp.Name

			for _, col := range tp.Columns {
				if spec := r.fieldToSpec(col, linesTable, tp.Name+".", def, targetEntityName); spec != nil {
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
			dbCol := toSnakeCase(field.Name)
			if !strings.HasSuffix(dbCol, "_id") {
				dbCol = dbCol + "_id"
			}
			return &refFieldSpec{
				tableName:  tableName,
				fieldName:  fieldPrefix + field.Name,
				isTypedRef: false,
				dbColumn:   dbCol,
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

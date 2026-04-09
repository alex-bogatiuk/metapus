package postgres

import (
	"context"
	"fmt"

	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/metadata"
	"metapus/pkg/logger"
)

// MarkedObjectsRepo implements the MarkedObjectsProcessor interface.
// Lists all deletion-marked entities and supports physical deletion.
type MarkedObjectsRepo struct {
	registry *metadata.Registry
	finder   *RefFinderRepo
	resolver *RefResolverRepo
}

// NewMarkedObjectsRepo creates a new MarkedObjectsRepo.
func NewMarkedObjectsRepo(registry *metadata.Registry) *MarkedObjectsRepo {
	return &MarkedObjectsRepo{
		registry: registry,
		finder:   NewRefFinderRepo(registry),
		resolver: NewRefResolverRepo(registry),
	}
}

// ListMarkedObjects scans all registered entities for deletion-marked rows.
// For each found object: resolves presentation and counts incoming references.
func (r *MarkedObjectsRepo) ListMarkedObjects(ctx context.Context) ([]domain.MarkedObject, error) {
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	results := make([]domain.MarkedObject, 0, 64)

	for _, def := range r.registry.List() {
		tableName := deriveTableName(def)
		if tableName == "" {
			continue
		}

		entityType := string(def.Type)

		// Query deletion-marked rows
		sql := fmt.Sprintf(
			`SELECT id FROM %s WHERE deletion_mark = TRUE ORDER BY id LIMIT 500`,
			tableName,
		)
		rows, err := querier.Query(ctx, sql)
		if err != nil {
			logger.Warn(ctx, "skip entity in marked scan", "entity", def.Name, "error", err)
			continue
		}

		ids := make([]id.ID, 0, 64)
		for rows.Next() {
			var uid id.ID
			if err := rows.Scan(&uid); err != nil {
				continue
			}
			ids = append(ids, uid)
		}
		rows.Close()

		if len(ids) == 0 {
			continue
		}

		// Resolve presentations in batch
		resolveReqs := make([]domain.RefResolveRequest, len(ids))
		for i, uid := range ids {
			resolveReqs[i] = domain.RefResolveRequest{RefType: def.Name, RefID: uid}
		}
		resolved, _ := r.resolver.ResolveRefs(ctx, resolveReqs)
		presMap := make(map[id.ID]string)
		for _, res := range resolved {
			presMap[res.RefID] = res.Presentation
		}

		// Count references for all marked objects in batch
		counts, _ := r.finder.CountReferencesBatch(ctx, def.Name, ids)

		for _, uid := range ids {
			refCount := counts[uid]
			results = append(results, domain.MarkedObject{
				EntityName:   def.Name,
				EntityType:   entityType,
				EntityID:     uid,
				Presentation: presMap[uid],
				RefCount:     refCount,
				CanDelete:    refCount == 0,
			})
		}
	}

	return results, nil
}

// DeleteMarked physically deletes specified entities.
// Only deletes entities with no incoming references (safety check).
func (r *MarkedObjectsRepo) DeleteMarked(ctx context.Context, items []domain.DeleteMarkedRequest) (domain.DeleteMarkedResult, error) {
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	var result domain.DeleteMarkedResult

	// Group items by EntityName
	groups := make(map[string][]id.ID)
	for _, item := range items {
		groups[item.EntityName] = append(groups[item.EntityName], item.EntityID)
	}

	for entityName, ids := range groups {
		// Resolve table name
		def, ok := r.registry.Get(entityName)
		if !ok {
			result.Errors += len(ids)
			continue
		}

		tableName := deriveTableName(def)
		if tableName == "" {
			result.Errors += len(ids)
			continue
		}

		// Safety: recheck references before deletion in batch
		counts, err := r.finder.CountReferencesBatch(ctx, entityName, ids)
		if err != nil {
			logger.Warn(ctx, "ref count batch failed during delete", "entity", entityName, "error", err)
			result.Errors += len(ids)
			continue
		}

		safeToDelete := make([]id.ID, 0, len(ids))
		for _, uid := range ids {
			if counts[uid] > 0 {
				result.Skipped++
			} else {
				safeToDelete = append(safeToDelete, uid)
			}
		}

		if len(safeToDelete) == 0 {
			continue
		}

		// Physical DELETE in batch
		sql := fmt.Sprintf(
			`DELETE FROM %s WHERE id = ANY($1) AND deletion_mark = TRUE`,
			tableName,
		)
		tag, err := querier.Exec(ctx, sql, safeToDelete)
		if err != nil {
			logger.Warn(ctx, "physical delete batch failed", "entity", entityName, "error", err)
			result.Errors += len(safeToDelete)
			continue
		}
		
		result.Deleted += int(tag.RowsAffected())
		
		// some may have been skipped if not actually marked
		if int(tag.RowsAffected()) < len(safeToDelete) {
			result.Skipped += len(safeToDelete) - int(tag.RowsAffected())
		}
	}

	return result, nil
}

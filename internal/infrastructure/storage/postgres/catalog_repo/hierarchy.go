package catalog_repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
)

// Advisory locks are scoped to the current tenant database and table OID.
// A separate namespace prevents collisions with other advisory-lock protocols.
func (r *BaseCatalogRepo[T]) LockHierarchy(ctx context.Context) error {
	if !r.hierarchical {
		return nil
	}
	txm := r.getTxManager(ctx)
	transaction := txm.GetTx(ctx)
	if transaction == nil {
		return fmt.Errorf("hierarchy lock requires a transaction")
	}
	// A snapshot established before waiting could miss a committed competing move.
	var isolation string
	if err := transaction.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	if isolation != string(pgx.ReadCommitted) {
		return apperror.NewValidation("hierarchy writes require read committed isolation")
	}
	_, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2::regclass::oid::int)", int32(0x4d485952), r.tableName)
	return err
}

// Repository-level enforcement also covers imports and batch callers that do
// not use CatalogService. Every writer of parent_id follows the same protocol.
func (r *BaseCatalogRepo[T]) withHierarchyWrite(ctx context.Context, value T, write func(context.Context) error) error {
	if !r.hierarchical {
		return write(ctx)
	}
	return r.getTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := r.LockHierarchy(ctx); err != nil {
			return err
		}
		meta := r.hierarchyMeta
		if !meta.Hierarchical {
			return fmt.Errorf("hierarchy definition is missing for %s", r.tableName)
		}
		accessor, ok := any(value).(domain.ParentAccessor)
		if !ok {
			return fmt.Errorf("hierarchical model has no parent accessor")
		}
		get := func(ctx context.Context, uid id.ID) (domain.ParentAccessor, error) {
			parent, err := r.GetByID(ctx, uid)
			if err != nil {
				return nil, err
			}
			return any(parent).(domain.ParentAccessor), nil
		}
		if err := domain.NewHierarchyValidator(meta).ValidateHierarchy(ctx, accessor, get); err != nil {
			return err
		}
		// Moving a branch must preserve the depth of every descendant, and a
		// folder with children cannot be converted into an item.
		uid := accessor.GetID()
		children, err := r.readTree(ctx, &uid, true)
		if err != nil {
			return err
		}
		if len(children) > 0 && meta.FolderAsParentOnly && !accessor.GetIsFolder() {
			return apperror.NewValidation("an element with children must remain a folder")
		}
		if meta.MaxDepth > 0 {
			parents := map[id.ID]domain.ParentAccessor{uid: accessor}
			for _, child := range children {
				p := any(child).(domain.ParentAccessor)
				parents[p.GetID()] = p
			}
			getMoved := func(ctx context.Context, uid id.ID) (domain.ParentAccessor, error) {
				if row, ok := parents[uid]; ok {
					return row, nil
				}
				return get(ctx, uid)
			}
			validator := domain.NewHierarchyValidator(meta)
			for _, child := range children {
				if err := validator.ValidateHierarchy(ctx, any(child).(domain.ParentAccessor), getMoved); err != nil {
					return err
				}
			}
		}
		return write(ctx)
	})
}

// Rootless cycles would otherwise disappear from a roots-first recursive read.
func (r *BaseCatalogRepo[T]) validateHierarchyGraph(ctx context.Context) error {
	rows, err := r.getTxManager(ctx).GetQuerier(ctx).Query(ctx, "SELECT id, parent_id FROM "+r.tableName)
	if err != nil {
		return err
	}
	defer rows.Close()
	parents := map[id.ID]*id.ID{}
	for rows.Next() {
		var uid id.ID
		var parent *id.ID
		if err := rows.Scan(&uid, &parent); err != nil {
			return err
		}
		parents[uid] = parent
	}
	if err := rows.Err(); err != nil {
		return err
	}
	complete := map[id.ID]bool{}
	for uid := range parents {
		path := map[id.ID]bool{}
		for current := &uid; current != nil && !complete[*current]; current = parents[*current] {
			if path[*current] {
				return apperror.NewValidation("catalog hierarchy contains a cycle")
			}
			path[*current] = true
		}
		for visited := range path {
			complete[visited] = true
		}
	}
	return nil
}

package postgres

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/domain/eventlog"
	"metapus/internal/metadata"
	"metapus/pkg/logger"
)

const (
	// markedScanLimit caps the marked objects listed per entity. The rest are
	// listed once the first ones are deleted.
	markedScanLimit = 500
	// liveReferenceSample is how many live referencing objects are listed per
	// referencing column; marked ones are always listed in full.
	liveReferenceSample = 5
	// MaxDeleteMarkedItems bounds one deletion request.
	MaxDeleteMarkedItems = 1000
)

// MarkedObjectsRepo implements domain.MarkedObjectsProcessor: it plans and
// performs physical deletion of deletion-marked catalogs and documents.
//
// References come from two places. The metadata registry names reference
// fields, including those without a foreign key. Foreign keys in the schema
// name the rest: a key from a table the registry does not describe still
// blocks deletion, since PostgreSQL would refuse the DELETE.
type MarkedObjectsRepo struct {
	registry *metadata.Registry
	finder   *RefFinderRepo
	resolver *RefResolverRepo
	events   eventlog.Writer
}

// NewMarkedObjectsRepo creates a MarkedObjectsRepo. events may be nil; when
// set, every physical deletion is recorded in the same transaction.
func NewMarkedObjectsRepo(registry *metadata.Registry, events eventlog.Writer) *MarkedObjectsRepo {
	return &MarkedObjectsRepo{
		registry: registry,
		finder:   NewRefFinderRepo(registry),
		resolver: NewRefResolverRepo(registry),
		events:   events,
	}
}

// referenceSource is one column that can hold references to a target table.
type referenceSource struct {
	table  string // table holding the column
	column string
	field  string // field shown to the user
	// typedRef sources keep the target type in ref_type next to ref_id.
	typedRef bool
	// owner is the entity whose objects hold the reference; nil for tables
	// the registry does not describe.
	owner *metadata.EntityDef
	// header is the owner's table when table is one of its table parts.
	header string
}

// schemaColumns records which tables carry the columns deletion relies on.
type schemaColumns struct {
	deletionMark map[string]bool
	posted       map[string]bool
}

func (r *MarkedObjectsRepo) loadSchemaColumns(ctx context.Context) (schemaColumns, error) {
	cols := schemaColumns{deletionMark: map[string]bool{}, posted: map[string]bool{}}
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND column_name IN ('deletion_mark', 'posted')`)
	if err != nil {
		return cols, fmt.Errorf("read deletion columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return cols, fmt.Errorf("read deletion columns: %w", err)
		}
		if column == "deletion_mark" {
			cols.deletionMark[table] = true
		} else {
			cols.posted[table] = true
		}
	}
	return cols, rows.Err()
}

// markableEntities lists catalogs and documents whose table carries a
// deletion mark, ordered by name so scans and locks run in a stable order.
func (r *MarkedObjectsRepo) markableEntities(cols schemaColumns) []metadata.EntityDef {
	defs := make([]metadata.EntityDef, 0, 32)
	for _, def := range r.registry.List() {
		if def.Type != metadata.TypeCatalog && def.Type != metadata.TypeDocument {
			continue
		}
		if table := deriveTableName(def); table != "" && cols.deletionMark[table] {
			defs = append(defs, def)
		}
	}
	slices.SortFunc(defs, func(a, b metadata.EntityDef) int { return cmp.Compare(a.Name, b.Name) })
	return defs
}

// referenceSources lists every column that may reference rows of target.
func (r *MarkedObjectsRepo) referenceSources(ctx context.Context, target metadata.EntityDef) ([]referenceSource, error) {
	sources := make([]referenceSource, 0, 16)
	covered := make(map[[2]string]bool)
	for _, spec := range r.finder.buildRefSpecs(target.Name) {
		owner := spec.entityDef
		source := referenceSource{table: spec.tableName, column: spec.dbColumn, field: spec.fieldName, typedRef: spec.isTypedRef, owner: &owner}
		if spec.isTypedRef {
			source.column = "ref_id"
		}
		if spec.isTablePart {
			source.header = deriveTableName(owner)
		}
		covered[[2]string{source.table, source.column}] = true
		sources = append(sources, source)
	}

	// Foreign keys that block a DELETE (NO ACTION / RESTRICT) and that the
	// metadata does not describe. Cascading keys own their rows and do not
	// block.
	rows, err := MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `
		SELECT src.relname, a.attname
		FROM pg_constraint c
		JOIN pg_class src ON src.oid = c.conrelid
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'f' AND cardinality(c.conkey) = 1
		  AND c.confrelid = to_regclass(quote_ident(current_schema()) || '.' || quote_ident($1))
		  AND c.confdeltype IN ('a', 'r')
		ORDER BY src.relname, a.attname`, deriveTableName(target))
	if err != nil {
		return nil, fmt.Errorf("read foreign keys of %s: %w", target.Name, err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, fmt.Errorf("read foreign keys of %s: %w", target.Name, err)
		}
		if covered[[2]string{table, column}] {
			continue
		}
		covered[[2]string{table, column}] = true
		source := referenceSource{table: table, column: column, field: table + "." + column}
		for _, def := range r.registry.List() {
			if deriveTableName(def) == table {
				owner := def
				source.owner, source.field = &owner, column
				break
			}
			if part := slices.IndexFunc(def.TableParts, func(tp metadata.TablePartDef) bool { return tp.SQLTable == table }); part >= 0 {
				owner := def
				source.owner, source.header = &owner, deriveTableName(def)
				source.field = def.TableParts[part].Name + "." + column
				break
			}
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

// incomingRef is one reference found for a target, before presentation.
type incomingRef struct {
	target   id.ID
	source   domain.MarkedObjectRef // zero for unmarkable sources
	kind     string
	field    string
	marked   bool
	posted   bool
	count    int
	liveRefs int // live references of this column to the target
}

// incomingReferences reads, for the target ids of one entity, every
// referencing object that carries a deletion mark and a sample of the live
// ones, with the number of live references per column.
func (r *MarkedObjectsRepo) incomingReferences(ctx context.Context, cols schemaColumns, target metadata.EntityDef, ids []id.ID) ([]incomingRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	sources, err := r.referenceSources(ctx, target)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, nil
	}

	batch := &pgx.Batch{}
	for _, source := range sources {
		sql, args := incomingQuery(cols, source, target.Name, ids)
		batch.Queue(sql, args...)
	}
	results := MustGetTxManager(ctx).GetQuerier(ctx).SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	found := make([]incomingRef, 0, len(ids))
	for _, source := range sources {
		rows, err := results.Query()
		if err != nil {
			return nil, fmt.Errorf("find references in %s: %w", source.table, err)
		}
		markable := source.owner != nil && cols.deletionMark[ownerTable(source)]
		kind := domain.SourceKindSystem
		if source.owner != nil {
			kind = string(source.owner.Type)
		}
		for rows.Next() {
			ref := incomingRef{kind: kind, field: source.field}
			if markable {
				var sourceID id.ID
				var liveTotal int64
				if err := rows.Scan(&ref.target, &sourceID, &ref.marked, &ref.posted, &liveTotal); err != nil {
					rows.Close()
					return nil, fmt.Errorf("find references in %s: %w", source.table, err)
				}
				ref.source = domain.MarkedObjectRef{EntityName: source.owner.Name, EntityID: sourceID}
				ref.count, ref.liveRefs = 1, int(liveTotal)
			} else {
				var count int64
				if err := rows.Scan(&ref.target, &count); err != nil {
					rows.Close()
					return nil, fmt.Errorf("find references in %s: %w", source.table, err)
				}
				if source.owner != nil {
					ref.source.EntityName = source.owner.Name
				}
				ref.count, ref.liveRefs = int(count), int(count)
			}
			found = append(found, ref)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("find references in %s: %w", source.table, err)
		}
	}
	return found, results.Close()
}

func ownerTable(source referenceSource) string {
	if source.header != "" {
		return source.header
	}
	return source.table
}

// incomingQuery builds the reference query for one source column.
func incomingQuery(cols schemaColumns, source referenceSource, targetName string, ids []id.ID) (string, []any) {
	table := pgx.Identifier{source.table}.Sanitize()
	column := pgx.Identifier{source.column}.Sanitize()
	args := []any{ids}
	typeFilter := ""
	if source.typedRef {
		typeFilter = " AND s.ref_type = $2"
		args = append(args, targetName)
	}
	if source.owner == nil || !cols.deletionMark[ownerTable(source)] {
		return fmt.Sprintf(`SELECT s.%s, count(*) FROM %s s WHERE s.%s = ANY($1)%s GROUP BY s.%s`,
			column, table, column, typeFilter, column), args
	}
	posted := "FALSE"
	if cols.posted[ownerTable(source)] {
		posted = "h.posted"
	}
	// Live references are sampled; marked ones are listed in full, since the
	// plan needs every one of them.
	const sampled = `SELECT target, source, marked, posted, live_total FROM (
		SELECT d.target, d.source, h.deletion_mark AS marked, %s AS posted,
		       count(*) FILTER (WHERE NOT h.deletion_mark) OVER (PARTITION BY d.target) AS live_total,
		       row_number() OVER (PARTITION BY d.target, h.deletion_mark ORDER BY d.source) AS rn
		FROM (%s) d JOIN %s h ON h.id = d.source
	) x WHERE marked OR rn <= %d`
	if source.header == "" {
		inner := fmt.Sprintf(`SELECT s.%s AS target, s.id AS source FROM %s s WHERE s.%s = ANY($1)%s`,
			column, table, column, typeFilter)
		return fmt.Sprintf(sampled, posted, inner, table, liveReferenceSample), args
	}
	inner := fmt.Sprintf(`SELECT DISTINCT s.%s AS target, s.document_id AS source FROM %s s WHERE s.%s = ANY($1)%s`,
		column, table, column, typeFilter)
	return fmt.Sprintf(sampled, posted, inner, pgx.Identifier{source.header}.Sanitize(), liveReferenceSample), args
}

// markedNode is one marked object found by the scan.
type markedNode struct {
	ref domain.MarkedObjectRef
	def metadata.EntityDef
}

// scanMarked lists the deletion-marked objects of every markable entity.
func (r *MarkedObjectsRepo) scanMarked(ctx context.Context, cols schemaColumns) ([]markedNode, bool, error) {
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	nodes := make([]markedNode, 0, 64)
	truncated := false
	for _, def := range r.markableEntities(cols) {
		sql := fmt.Sprintf(`SELECT id FROM %s WHERE deletion_mark ORDER BY id LIMIT %d`,
			pgx.Identifier{deriveTableName(def)}.Sanitize(), markedScanLimit+1)
		rows, err := querier.Query(ctx, sql)
		if err != nil {
			return nil, false, fmt.Errorf("scan marked %s: %w", def.Name, err)
		}
		count := 0
		for rows.Next() {
			var uid id.ID
			if err := rows.Scan(&uid); err != nil {
				rows.Close()
				return nil, false, fmt.Errorf("scan marked %s: %w", def.Name, err)
			}
			count++
			if count > markedScanLimit {
				truncated = true
				break
			}
			nodes = append(nodes, markedNode{ref: domain.MarkedObjectRef{EntityName: def.Name, EntityID: uid}, def: def})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, false, fmt.Errorf("scan marked %s: %w", def.Name, err)
		}
	}
	return nodes, truncated, nil
}

// buildGraph reads the incoming references of nodes and returns the graph
// together with the raw references per node.
func (r *MarkedObjectsRepo) buildGraph(ctx context.Context, cols schemaColumns, nodes []markedNode, selected map[domain.MarkedObjectRef]bool) (domain.DeletionGraph, map[domain.MarkedObjectRef][]incomingRef, error) {
	graph := domain.DeletionGraph{
		Marked:   make([]domain.MarkedObjectRef, 0, len(nodes)),
		Selected: selected,
		Incoming: make(map[domain.MarkedObjectRef][]domain.DeletionEdge, len(nodes)),
	}
	byNode := make(map[domain.MarkedObjectRef][]incomingRef, len(nodes))
	byEntity := make(map[string][]id.ID)
	defs := make(map[string]metadata.EntityDef)
	var entityOrder []string
	for _, node := range nodes {
		graph.Marked = append(graph.Marked, node.ref)
		if _, ok := defs[node.ref.EntityName]; !ok {
			defs[node.ref.EntityName] = node.def
			entityOrder = append(entityOrder, node.ref.EntityName)
		}
		byEntity[node.ref.EntityName] = append(byEntity[node.ref.EntityName], node.ref.EntityID)
	}
	for _, name := range entityOrder {
		refs, err := r.incomingReferences(ctx, cols, defs[name], byEntity[name])
		if err != nil {
			return graph, nil, err
		}
		for _, ref := range refs {
			target := domain.MarkedObjectRef{EntityName: name, EntityID: ref.target}
			byNode[target] = append(byNode[target], ref)
			edge := domain.DeletionEdge{Live: !ref.marked || id.IsNil(ref.source.EntityID)}
			if !id.IsNil(ref.source.EntityID) {
				edge.Source = ref.source
			}
			graph.Incoming[target] = append(graph.Incoming[target], edge)
		}
	}
	return graph, byNode, nil
}

// presentations resolves display strings for marked objects and their
// sources, including marked ones. Objects the user may not read stay empty.
func (r *MarkedObjectsRepo) presentations(ctx context.Context, refs []domain.MarkedObjectRef) (map[domain.MarkedObjectRef]string, error) {
	requests := make([]domain.RefResolveRequest, 0, len(refs))
	seen := make(map[domain.MarkedObjectRef]bool, len(refs))
	for _, ref := range refs {
		if ref.EntityName == "" || id.IsNil(ref.EntityID) || seen[ref] {
			continue
		}
		seen[ref] = true
		requests = append(requests, domain.RefResolveRequest{RefType: ref.EntityName, RefID: ref.EntityID})
	}
	resolved, err := r.resolver.ResolveRefsWithDeletionMarked(ctx, requests)
	if err != nil {
		return nil, fmt.Errorf("resolve marked objects: %w", err)
	}
	out := make(map[domain.MarkedObjectRef]string, len(resolved))
	for _, row := range resolved {
		out[domain.MarkedObjectRef{EntityName: row.RefType, EntityID: row.RefID}] = row.Presentation
	}
	return out, nil
}

// PlanDeletion lists every marked object with its references and the status
// it gets when the selection is deleted.
func (r *MarkedObjectsRepo) PlanDeletion(ctx context.Context, selection []domain.MarkedObjectRef) (domain.DeletionPlan, error) {
	var plan domain.DeletionPlan
	// One read-only transaction gives the scan and the reference queries one
	// snapshot; inside a caller's transaction its snapshot is used instead.
	txm := MustGetTxManager(ctx)
	run := txm.ReadOnly
	if txm.GetTx(ctx) != nil {
		run = func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
	}
	err := run(ctx, func(ctx context.Context) error {
		cols, err := r.loadSchemaColumns(ctx)
		if err != nil {
			return err
		}
		nodes, truncated, err := r.scanMarked(ctx, cols)
		if err != nil {
			return err
		}
		selected := make(map[domain.MarkedObjectRef]bool, len(nodes))
		if selection == nil {
			for _, node := range nodes {
				selected[node.ref] = true
			}
		} else {
			for _, ref := range selection {
				selected[ref] = true
			}
		}
		graph, refs, err := r.buildGraph(ctx, cols, nodes, selected)
		if err != nil {
			return err
		}
		schedule := domain.PlanMarkedDeletion(graph)

		toResolve := slices.Clone(graph.Marked)
		for _, list := range refs {
			for _, ref := range list {
				toResolve = append(toResolve, ref.source)
			}
		}
		names, err := r.presentations(ctx, toResolve)
		if err != nil {
			return err
		}

		plan = domain.DeletionPlan{Items: make([]domain.MarkedObject, 0, len(nodes)), Total: len(nodes), Truncated: truncated}
		for _, node := range nodes {
			item := r.describe(node, refs[node.ref], graph, schedule, names)
			switch item.Status {
			case domain.DeletionReady, domain.DeletionWithGroup:
				plan.Deletable++
				plan.Selected++
			case domain.DeletionBlocked:
				plan.Blocked++
				plan.Selected++
			}
			plan.Items = append(plan.Items, item)
		}
		return nil
	})
	return plan, err
}

// describe assembles the API view of one marked object.
func (r *MarkedObjectsRepo) describe(node markedNode, refs []incomingRef, graph domain.DeletionGraph, schedule domain.DeletionSchedule, names map[domain.MarkedObjectRef]string) domain.MarkedObject {
	decision := schedule.Decisions[node.ref]
	item := domain.MarkedObject{
		EntityName:   node.ref.EntityName,
		EntityType:   string(node.def.Type),
		EntityID:     node.ref.EntityID,
		Presentation: names[node.ref],
		Status:       decision.Status,
		Reason:       decision.Reason,
		References:   make([]domain.IncomingReference, 0, len(refs)),
	}
	// Live references are sampled per referencing column; key by entity and
	// field, since different entities may name their columns alike.
	liveByField := make(map[string]int)
	liveListed := make(map[string]int)
	for _, ref := range refs {
		view := domain.IncomingReference{
			SourceEntityName: ref.source.EntityName,
			SourceEntityType: ref.kind,
			SourceID:         ref.source.EntityID,
			SourceField:      ref.field,
			Presentation:     names[ref.source],
			SourceMarked:     ref.marked,
			SourcePosted:     ref.posted,
			Count:            ref.count,
		}
		if ref.marked {
			view.SourceStatus = schedule.Decisions[ref.source].Status
			if view.SourceStatus == "" {
				// Marked, but beyond the scan limit or not catalog/document.
				view.SourceStatus = domain.DeletionExcluded
			}
		}
		if id.IsNil(ref.source.EntityID) {
			item.LiveRefCount += ref.count
		} else if !ref.marked {
			key := ref.source.EntityName + "|" + ref.field
			liveByField[key] = ref.liveRefs
			liveListed[key]++
		}
		item.RefCount += ref.count
		item.References = append(item.References, view)
	}
	for field, total := range liveByField {
		item.LiveRefCount += total
		item.RefCount += total - liveListed[field]
		if total > liveListed[field] {
			item.ReferencesTruncated = true
		}
	}
	if decision.Blocker >= 0 && decision.Blocker < len(item.References) {
		blocker := item.References[decision.Blocker]
		item.BlockedBy = &blocker
	}
	return item
}

// DeleteMarked deletes the requested objects that the plan allows. Each
// independent group of objects is deleted in its own savepoint, so one group
// failing leaves the others deleted.
func (r *MarkedObjectsRepo) DeleteMarked(ctx context.Context, items []domain.MarkedObjectRef) (domain.DeleteMarkedResult, error) {
	result := domain.DeleteMarkedResult{Items: make([]domain.DeletedObjectResult, 0, len(items))}
	seen := make(map[domain.MarkedObjectRef]bool, len(items))
	items = slices.DeleteFunc(slices.Clone(items), func(item domain.MarkedObjectRef) bool {
		duplicate := seen[item]
		seen[item] = true
		return duplicate
	})
	if len(items) > MaxDeleteMarkedItems {
		return result, fmt.Errorf("too many objects to delete: %d > %d", len(items), MaxDeleteMarkedItems)
	}
	txm := MustGetTxManager(ctx)
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		result = domain.DeleteMarkedResult{Items: make([]domain.DeletedObjectResult, 0, len(items))}
		cols, err := r.loadSchemaColumns(ctx)
		if err != nil {
			return err
		}
		nodes, outcomes, err := r.lockMarked(ctx, cols, items)
		if err != nil {
			return err
		}
		selected := make(map[domain.MarkedObjectRef]bool, len(nodes))
		for _, node := range nodes {
			selected[node.ref] = true
		}
		graph, _, err := r.buildGraph(ctx, cols, nodes, selected)
		if err != nil {
			return err
		}
		schedule := domain.PlanMarkedDeletion(graph)

		refs := make([]domain.MarkedObjectRef, 0, len(items))
		for _, node := range nodes {
			refs = append(refs, node.ref)
		}
		names, err := r.presentations(ctx, refs)
		if err != nil {
			// Names only decorate the report; deletion does not depend on them.
			logger.Warn(ctx, "resolve names of deleted objects", "error", err)
			names = map[domain.MarkedObjectRef]string{}
		}
		defs := make(map[domain.MarkedObjectRef]metadata.EntityDef, len(nodes))
		for _, node := range nodes {
			defs[node.ref] = node.def
			if decision := schedule.Decisions[node.ref]; decision.Status == domain.DeletionBlocked {
				outcomes[node.ref] = domain.DeletedObjectResult{Outcome: domain.DeletionOutcomeSkipped, Reason: decision.Reason, Message: blockMessage(decision.Reason)}
			}
		}

		for _, group := range schedule.Groups {
			err := txm.RunInSavepoint(ctx, func(ctx context.Context) error {
				for _, layer := range group {
					if err := r.deleteLayer(ctx, cols, defs, layer); err != nil {
						return err
					}
				}
				return nil
			})
			message := ""
			outcome := domain.DeletionOutcomeDeleted
			if err != nil {
				outcome, message = domain.DeletionOutcomeFailed, deleteFailureMessage(err)
				logger.Warn(ctx, "delete marked group failed", "objects", len(slices.Concat(group...)), "error", err)
			}
			for _, layer := range group {
				for _, ref := range layer {
					outcomes[ref] = domain.DeletedObjectResult{Outcome: outcome, Message: message}
				}
			}
		}

		for _, item := range items {
			outcome := outcomes[item]
			outcome.EntityName, outcome.EntityID, outcome.Presentation = item.EntityName, item.EntityID, names[item]
			switch outcome.Outcome {
			case domain.DeletionOutcomeDeleted:
				result.Deleted++
			case domain.DeletionOutcomeSkipped:
				result.Skipped++
			default:
				result.Errors++
			}
			result.Items = append(result.Items, outcome)
		}
		return nil
	})
	return result, err
}

// lockMarked locks the requested rows that still carry a deletion mark and
// reports the rest. Entities are locked in name order and rows in id order,
// so concurrent runs cannot deadlock.
func (r *MarkedObjectsRepo) lockMarked(ctx context.Context, cols schemaColumns, items []domain.MarkedObjectRef) ([]markedNode, map[domain.MarkedObjectRef]domain.DeletedObjectResult, error) {
	outcomes := make(map[domain.MarkedObjectRef]domain.DeletedObjectResult, len(items))
	requested := make(map[string][]id.ID)
	for _, item := range items {
		outcomes[item] = domain.DeletedObjectResult{Outcome: domain.DeletionOutcomeSkipped, Message: "Объект не найден или с него снята пометка удаления"}
		requested[item.EntityName] = append(requested[item.EntityName], item.EntityID)
	}
	markable := make(map[string]metadata.EntityDef)
	for _, def := range r.markableEntities(cols) {
		markable[def.Name] = def
	}
	names := make([]string, 0, len(requested))
	for name := range requested {
		names = append(names, name)
	}
	slices.Sort(names)

	nodes := make([]markedNode, 0, len(items))
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	for _, name := range names {
		def, ok := markable[name]
		if !ok {
			for _, uid := range requested[name] {
				outcomes[domain.MarkedObjectRef{EntityName: name, EntityID: uid}] = domain.DeletedObjectResult{
					Outcome: domain.DeletionOutcomeFailed, Message: "Этот тип объектов нельзя удалить обработкой",
				}
			}
			continue
		}
		table := deriveTableName(def)
		notPosted := ""
		if cols.posted[table] {
			notPosted = " AND NOT posted"
		}
		sql := fmt.Sprintf(`SELECT id FROM %s WHERE id = ANY($1) AND deletion_mark%s ORDER BY id FOR UPDATE`,
			pgx.Identifier{table}.Sanitize(), notPosted)
		rows, err := querier.Query(ctx, sql, requested[name])
		if err != nil {
			return nil, nil, fmt.Errorf("lock marked %s: %w", name, err)
		}
		for rows.Next() {
			var uid id.ID
			if err := rows.Scan(&uid); err != nil {
				rows.Close()
				return nil, nil, fmt.Errorf("lock marked %s: %w", name, err)
			}
			nodes = append(nodes, markedNode{ref: domain.MarkedObjectRef{EntityName: name, EntityID: uid}, def: def})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, nil, fmt.Errorf("lock marked %s: %w", name, err)
		}
	}
	return nodes, outcomes, nil
}

// deleteLayer deletes one layer, one statement per table. Table parts go
// first; objects of one table that reference each other go in one statement,
// because PostgreSQL checks NO ACTION keys at the end of the statement.
func (r *MarkedObjectsRepo) deleteLayer(ctx context.Context, cols schemaColumns, defs map[domain.MarkedObjectRef]metadata.EntityDef, layer []domain.MarkedObjectRef) error {
	querier := MustGetTxManager(ctx).GetQuerier(ctx)
	byEntity := make(map[string][]id.ID)
	var order []string
	for _, ref := range layer {
		if _, ok := byEntity[ref.EntityName]; !ok {
			order = append(order, ref.EntityName)
		}
		byEntity[ref.EntityName] = append(byEntity[ref.EntityName], ref.EntityID)
	}
	for _, name := range order {
		ids := byEntity[name]
		def := defs[domain.MarkedObjectRef{EntityName: name, EntityID: ids[0]}]
		for _, part := range def.TableParts {
			if part.SQLTable == "" {
				continue
			}
			if _, err := querier.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE document_id = ANY($1)`, pgx.Identifier{part.SQLTable}.Sanitize()), ids); err != nil {
				return fmt.Errorf("delete %s.%s: %w", name, part.Name, err)
			}
		}
		table := deriveTableName(def)
		notPosted := ""
		if cols.posted[table] {
			notPosted = " AND NOT posted"
		}
		tag, err := querier.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = ANY($1) AND deletion_mark%s`, pgx.Identifier{table}.Sanitize(), notPosted), ids)
		if err != nil {
			return fmt.Errorf("delete %s: %w", name, err)
		}
		if int(tag.RowsAffected()) != len(ids) {
			return fmt.Errorf("delete %s: %d of %d rows: %w", name, tag.RowsAffected(), len(ids), errDeletionRaced)
		}
		if err := r.recordDeletions(ctx, def, ids); err != nil {
			return err
		}
	}
	return nil
}

var errDeletionRaced = errors.New("rows changed during deletion")

// recordDeletions writes one audit event per deleted object. Like the
// catalog event log, a failed write rolls the deletion back.
func (r *MarkedObjectsRepo) recordDeletions(ctx context.Context, def metadata.EntityDef, ids []id.ID) error {
	if r.events == nil {
		return nil
	}
	eventType := eventlog.EventCatalogDelete
	if def.Type == metadata.TypeDocument {
		eventType = eventlog.EventDocumentDelete
	}
	for _, uid := range ids {
		uid := uid
		if err := r.events.Write(ctx, eventlog.Event{
			Category: eventlog.CategoryData, Severity: eventlog.SeverityInfo,
			EventType: eventType, EntityType: def.Name, EntityID: &uid,
			UserID:  appctx.GetUserID(ctx),
			Details: map[string]any{"operation": "delete_marked"},
			Message: fmt.Sprintf("%s: %s (delete marked)", def.Name, eventType),
		}); err != nil {
			return fmt.Errorf("record deletion of %s: %w", def.Name, err)
		}
	}
	return nil
}

func blockMessage(reason domain.BlockReason) string {
	switch reason {
	case domain.BlockLiveReference:
		return "Объект используется в данных без пометки удаления"
	case domain.BlockNotSelected:
		return "На объект ссылается помеченный объект, который не выбран для удаления"
	case domain.BlockChain:
		return "На объект ссылается объект, который нельзя удалить"
	case domain.BlockCycle:
		return "Объекты разных типов ссылаются друг на друга по кругу"
	default:
		return "Удаление невозможно"
	}
}

// deleteFailureMessage explains a failed group without exposing SQL details;
// the details go to the server log.
func deleteFailureMessage(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23503":
		return "Объект используется в данных, которые не попали в план удаления"
	case errors.Is(err, errDeletionRaced):
		return "Объект изменился во время удаления. Обновите список и повторите"
	default:
		return "Не удалось удалить объект"
	}
}

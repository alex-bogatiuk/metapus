package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

// maxTreeDepth is the maximum depth of the subordination tree traversal.
const _maxTreeDepth = 10

// maxTreeNodes is the maximum total nodes in the tree (safety limit).
const _maxTreeNodes = 100

// maxItemsPerGroup is the maximum number of items returned per flat group.
const _maxItemsPerGroup = 5

// RelatedDocRepo adapts RefFinderRepo to provide the document subordination tree.
// It traverses basis_type / basis_id links to build a hierarchical tree
// similar to 1C's "Структура подчинённости документа".
//
// Algorithm:
//  1. Walk UP from the current document via basis_type/basis_id to find the root
//  2. Walk DOWN from root via BFS to build the full tree
//  3. Batch-resolve all nodes for presentation, amounts, preview data
//
// Additionally, FK-references (via RefFinder) that are NOT part of the
// basis chain are returned as flat groups.
type RelatedDocRepo struct {
	finder   *RefFinderRepo
	registry *metadata.Registry
}

// NewRelatedDocRepo creates a RelatedDocRepo.
func NewRelatedDocRepo(registry *metadata.Registry) *RelatedDocRepo {
	return &RelatedDocRepo{
		finder:   NewRefFinderRepo(registry),
		registry: registry,
	}
}

// treeKey uniquely identifies a document in the tree.
type treeKey struct {
	entityName string
	entityID   id.ID
}

// rawTreeNode is used during tree construction before batch-resolve.
type rawTreeNode struct {
	key      treeKey
	def      metadata.EntityDef
	children []*rawTreeNode
}

// FindRelatedDocuments returns the full subordination tree plus flat FK-references.
func (r *RelatedDocRepo) FindRelatedDocuments(ctx context.Context, req domain.RelatedDocumentsRequest) (*domain.RelatedDocumentsResult, error) {
	querier := MustGetTxManager(ctx).GetQuerier(ctx)

	// ── Step 1: Walk UP to find the root of the chain ──
	rootKey := treeKey{entityName: req.EntityName, entityID: req.EntityID}
	if def, ok := r.registry.Get(rootKey.entityName); !ok || def.Type != metadata.TypeDocument {
		return nil, fmt.Errorf("related documents: entity %q is not a registered document", rootKey.entityName)
	}
	visited := map[treeKey]bool{rootKey: true}

	for range _maxTreeDepth {
		def, ok := r.registry.Get(rootKey.entityName)
		if !ok || def.Type != metadata.TypeDocument {
			break
		}
		tableName := deriveTableName(def)
		if tableName == "" {
			break
		}

		var basisType string
		var basisID *id.ID
		query := fmt.Sprintf(`SELECT basis_type, basis_id FROM %s WHERE id = $1`, tableName)
		row := querier.QueryRow(ctx, query, rootKey.entityID)
		if err := row.Scan(&basisType, &basisID); err != nil {
			return nil, fmt.Errorf("read document basis for %s %s: %w", rootKey.entityName, rootKey.entityID, err)
		}
		if basisType == "" || basisID == nil {
			break // no parent — this is the root
		}

		parentKey := treeKey{entityName: basisType, entityID: *basisID}
		parentDef, ok := r.registry.Get(parentKey.entityName)
		if !ok || parentDef.Type != metadata.TypeDocument || deriveTableName(parentDef) == "" {
			break // internal or stale basis types are not part of the document tree
		}
		if visited[parentKey] {
			break // cycle protection
		}
		visited[parentKey] = true
		rootKey = parentKey
	}

	// ── Step 2: Build tree from root DOWN via BFS ──
	rootDef, ok := r.registry.Get(rootKey.entityName)
	if !ok || rootDef.Type != metadata.TypeDocument {
		return nil, fmt.Errorf("related documents: root %q is not a registered document", rootKey.entityName)
	}
	root := &rawTreeNode{key: rootKey, def: rootDef}

	totalNodes := 1
	queue := []*rawTreeNode{root}

	// All keys for batch resolve
	allKeys := []treeKey{rootKey}
	allKeysSet := map[treeKey]bool{rootKey: true}

	for len(queue) > 0 && totalNodes < _maxTreeNodes {
		// Process entire BFS level at once: collect all parent nodes
		currentLevel := queue
		queue = nil

		// Group parents by entityName for batch query
		type parentRef struct {
			entityName string
			entityID   id.ID
			node       *rawTreeNode
		}
		parentsByType := make(map[string][]parentRef)
		for _, node := range currentLevel {
			parentsByType[node.key.entityName] = append(parentsByType[node.key.entityName],
				parentRef{entityName: node.key.entityName, entityID: node.key.entityID, node: node})
		}

		// Collect all basis_types and basis_ids from this level
		var basisTypes []string
		var basisIDs []id.ID
		for basisType, refs := range parentsByType {
			for _, ref := range refs {
				basisTypes = append(basisTypes, basisType)
				basisIDs = append(basisIDs, ref.entityID)
			}
		}

		if len(basisIDs) == 0 {
			break
		}

		// Single UNION ALL query per BFS level (eliminates N+1 queries)
		var unionQueries []string
		for _, def := range r.registry.List() {
			if def.Type != metadata.TypeDocument || totalNodes >= _maxTreeNodes {
				continue
			}
			tableName := deriveTableName(def)
			if tableName == "" {
				continue
			}
			unionQueries = append(unionQueries, fmt.Sprintf(
				`SELECT '%s' as child_type, id, basis_type, basis_id FROM %s WHERE (basis_type, basis_id) IN (SELECT unnest($1::text[]), unnest($2::uuid[]))`,
				def.Name, tableName,
			))
		}

		if len(unionQueries) == 0 {
			break
		}

		query := fmt.Sprintf("%s LIMIT %d", strings.Join(unionQueries, " UNION ALL "), _maxTreeNodes-totalNodes)
		rows, err := querier.Query(ctx, query, basisTypes, basisIDs)
		if err != nil {
			return nil, fmt.Errorf("query related document children: %w", err)
		}

		for rows.Next() {
			if totalNodes >= _maxTreeNodes {
				break
			}
			var childType string
			var childID id.ID
			var basisType string
			var basisID id.ID
			if err := rows.Scan(&childType, &childID, &basisType, &basisID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan related document child: %w", err)
			}
			childKey := treeKey{entityName: childType, entityID: childID}
			if allKeysSet[childKey] {
				continue
			}

			// Find parent node by basisType + basisID
			var parentNode *rawTreeNode
			for _, ref := range parentsByType[basisType] {
				if ref.entityID == basisID {
					parentNode = ref.node
					break
				}
			}
			if parentNode == nil {
				continue
			}

			def, _ := r.registry.Get(childType)
			child := &rawTreeNode{key: childKey, def: def}
			parentNode.children = append(parentNode.children, child)
			queue = append(queue, child)
			allKeys = append(allKeys, childKey)
			allKeysSet[childKey] = true
			totalNodes++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate related document children: %w", err)
		}
		rows.Close()
	}

	// ── Step 3: Batch-resolve all nodes ──
	resolver := NewRefResolverRepo(r.registry)
	resolveRequests := make([]domain.RefResolveRequest, len(allKeys))
	for i, k := range allKeys {
		resolveRequests[i] = domain.RefResolveRequest{RefType: k.entityName, RefID: k.entityID}
	}

	resolveResults, err := resolver.ResolveRefs(ctx, resolveRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to batch resolve tree nodes: %w", err)
	}

	// Index results
	resolvedMap := make(map[treeKey]domain.RefResolveResult, len(resolveResults))
	for _, res := range resolveResults {
		resolvedMap[treeKey{entityName: res.RefType, entityID: res.RefID}] = res
	}
	isAuthorizedDoc := func(key treeKey) bool {
		res, ok := resolvedMap[key]
		return ok && res.EntityType == string(metadata.TypeDocument) && res.Presentation != ""
	}

	// ── Step 4: Convert rawTreeNode → RelatedDocTreeNode (pruning unauthorized nodes) ──
	currentKey := treeKey{entityName: req.EntityName, entityID: req.EntityID}
	if !isAuthorizedDoc(currentKey) {
		return nil, fmt.Errorf("resolve related document %s %s: document not found", currentKey.entityName, currentKey.entityID)
	}

	visibleRoot := highestAuthorizedAncestor(root, currentKey, isAuthorizedDoc)
	if visibleRoot == nil {
		return nil, fmt.Errorf("resolve related document %s %s: document not found", currentKey.entityName, currentKey.entityID)
	}

	var buildNode func(raw *rawTreeNode) domain.RelatedDocTreeNode
	buildNode = func(raw *rawTreeNode) domain.RelatedDocTreeNode {
		res := resolvedMap[raw.key]
		node := domain.RelatedDocTreeNode{
			ID:           raw.key.entityID,
			Presentation: res.Presentation,
			Number:       res.Number,
			Date:         res.Date,
			Posted:       res.Posted,
			DeletionMark: res.DeletionMark,
			Amount:       res.Amount,
			CurrencyID:   res.CurrencyID,
			PreviewData:  res.PreviewData,
			EntityName:   raw.key.entityName,
			EntityType:   string(raw.def.Type),
			RoutePrefix:  raw.def.RoutePrefix,
			IsCurrent:    raw.key == currentKey,
		}

		if len(raw.children) > 0 {
			authorizedChildren := make([]*rawTreeNode, 0, len(raw.children))
			for _, child := range raw.children {
				if isAuthorizedDoc(child.key) {
					authorizedChildren = append(authorizedChildren, child)
				}
			}
			if len(authorizedChildren) > 0 {
				// Sort children by date for stable output
				sort.Slice(authorizedChildren, func(i, j int) bool {
					ri := resolvedMap[authorizedChildren[i].key]
					rj := resolvedMap[authorizedChildren[j].key]
					if ri.Date.Equal(rj.Date) {
						return ri.Number < rj.Number
					}
					return ri.Date.Before(rj.Date)
				})

				node.Children = make([]domain.RelatedDocTreeNode, len(authorizedChildren))
				for i, child := range authorizedChildren {
					node.Children[i] = buildNode(child)
				}
			}
		}

		return node
	}

	treeRoot := buildNode(visibleRoot)

	// ── Step 5: FK-references NOT in the basis chain (flat groups) ──
	// Batch-resolve all FK refs in a single call (instead of N individual calls).
	var flatGroups []domain.RelatedDocGroup

	refs, err := r.finder.FindReferences(ctx, domain.FindReferencesRequest(req))
	if err != nil {
		return nil, fmt.Errorf("find related document references: %w", err)
	}
	{
		// Collect unique FK refs for batch resolve
		type fkEntry struct {
			entityName string
			sourceID   id.ID
			fallback   string // presentation fallback from FindReferences
		}
		var fkEntries []fkEntry
		seenFK := make(map[treeKey]bool)

		for _, ref := range refs {
			if ref.SourceEntityType != "document" {
				continue
			}
			refKey := treeKey{entityName: ref.SourceEntityName, entityID: ref.SourceID}
			if allKeysSet[refKey] || seenFK[refKey] {
				continue // already in the tree or duplicate
			}
			seenFK[refKey] = true
			fkEntries = append(fkEntries, fkEntry{
				entityName: ref.SourceEntityName,
				sourceID:   ref.SourceID,
				fallback:   ref.Presentation,
			})
		}

		// Batch resolve all FK refs in one call
		resolvedFK := make(map[treeKey]domain.RefResolveResult, len(fkEntries))
		if len(fkEntries) > 0 {
			resolveReqs := make([]domain.RefResolveRequest, len(fkEntries))
			for i, e := range fkEntries {
				resolveReqs[i] = domain.RefResolveRequest{RefType: e.entityName, RefID: e.sourceID}
			}
			fkResults, err := resolver.ResolveRefs(ctx, resolveReqs)
			if err != nil {
				return nil, fmt.Errorf("resolve related document references: %w", err)
			}
			for _, res := range fkResults {
				resolvedFK[treeKey{entityName: res.RefType, entityID: res.RefID}] = res
			}
		}

		// Build groups from resolved data
		type groupAcc struct {
			items []domain.RelatedDocItem
			def   metadata.EntityDef
		}
		groups := make(map[string]*groupAcc)

		for _, entry := range fkEntries {
			key := treeKey{entityName: entry.entityName, entityID: entry.sourceID}
			acc, ok := groups[entry.entityName]
			if !ok {
				def, _ := r.registry.Get(entry.entityName)
				acc = &groupAcc{def: def}
				groups[entry.entityName] = acc
			}

			var item domain.RelatedDocItem
			if res, found := resolvedFK[key]; found {
				item = domain.RelatedDocItem{
					ID:           res.RefID,
					Presentation: res.Presentation,
					Number:       res.Number,
					Date:         res.Date,
					Posted:       res.Posted,
					DeletionMark: res.DeletionMark,
					Amount:       res.Amount,
					CurrencyID:   res.CurrencyID,
					PreviewData:  res.PreviewData,
				}
			} else {
				continue // related row became inaccessible; never return a raw-ID fallback
			}
			acc.items = append(acc.items, item)
		}

		for entityName, acc := range groups {
			pres := acc.def.Presentation.Plural
			if pres == "" {
				pres = entityName
			}

			group := domain.RelatedDocGroup{
				EntityName:   entityName,
				EntityType:   string(acc.def.Type),
				Presentation: pres,
				RoutePrefix:  acc.def.RoutePrefix,
				TotalCount:   len(acc.items),
			}
			if len(acc.items) > _maxItemsPerGroup {
				group.Items = acc.items[:_maxItemsPerGroup]
			} else {
				group.Items = acc.items
			}
			flatGroups = append(flatGroups, group)
		}

		sort.Slice(flatGroups, func(i, j int) bool {
			return flatGroups[i].Presentation < flatGroups[j].Presentation
		})
	}

	return &domain.RelatedDocumentsResult{
		Tree:       &treeRoot,
		FlatGroups: flatGroups,
		Total:      countVisibleTreeNodes(treeRoot) + len(flatGroups),
	}, nil
}

// highestAuthorizedAncestor finds the path from root to target and returns the
// highest ancestor on a contiguous chain of authorized nodes leading to target.
func highestAuthorizedAncestor(root *rawTreeNode, target treeKey, isAuthorized func(treeKey) bool) *rawTreeNode {
	var path []*rawTreeNode
	var findPath func(node *rawTreeNode) bool
	findPath = func(node *rawTreeNode) bool {
		path = append(path, node)
		if node.key == target {
			return true
		}
		for _, child := range node.children {
			if findPath(child) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if !findPath(root) || len(path) == 0 {
		return nil
	}
	best := path[len(path)-1]
	for i := len(path) - 2; i >= 0; i-- {
		if !isAuthorized(path[i].key) {
			break
		}
		best = path[i]
	}
	return best
}

func countVisibleTreeNodes(node domain.RelatedDocTreeNode) int {
	n := 1
	for _, child := range node.Children {
		n += countVisibleTreeNodes(child)
	}
	return n
}

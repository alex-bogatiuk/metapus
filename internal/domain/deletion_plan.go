package domain

import "slices"

// DeletionEdge is one incoming reference of a marked object.
type DeletionEdge struct {
	// Source is the referencing object; zero for rows of system tables.
	Source MarkedObjectRef
	// Live means the source stays in the database whatever is selected: it
	// carries no deletion mark, or it cannot carry one.
	Live bool
}

// DeletionGraph is the input of PlanMarkedDeletion.
type DeletionGraph struct {
	// Marked lists every marked object the scan found, in a stable order.
	Marked []MarkedObjectRef
	// Selected holds the objects the user wants deleted.
	Selected map[MarkedObjectRef]bool
	// Incoming lists, per marked object, everything that references it.
	Incoming map[MarkedObjectRef][]DeletionEdge
}

// DeletionDecision is the planned status of one marked object.
type DeletionDecision struct {
	Status DeletionStatus
	Reason BlockReason
	// Blocker indexes the incoming edge that blocked the object, or is -1.
	Blocker int
}

// DeletionSchedule is the plan for a DeletionGraph.
type DeletionSchedule struct {
	Decisions map[MarkedObjectRef]DeletionDecision
	// Groups are independent sets of deletable objects: nothing in one group
	// references anything in another, so each group can succeed or fail on
	// its own. A group is a list of layers. Nothing in a layer is referenced
	// by an object of a later layer, so running the layers in order deletes
	// referencing objects before the objects they reference. Objects that
	// reference each other share a layer and are deleted by one statement,
	// which is possible only within one table.
	Groups [][][]MarkedObjectRef
}

// PlanMarkedDeletion decides which selected marked objects can be deleted.
//
// A selected object is deletable when everything that references it is
// itself deletable — the rule of 1C's "Удаление помеченных объектов": a
// reference from an object deleted in the same run does not count. The set
// of deletable objects is the greatest fixed point: start with the whole
// selection and drop objects with a reference from outside the set until
// nothing changes. Objects of different tables that reference each other in
// a cycle are dropped too, since no statement order satisfies their foreign
// keys.
func PlanMarkedDeletion(g DeletionGraph) DeletionSchedule {
	decisions := make(map[MarkedObjectRef]DeletionDecision, len(g.Marked))
	deletable := make(map[MarkedObjectRef]bool, len(g.Marked))
	for _, ref := range g.Marked {
		if g.Selected[ref] {
			deletable[ref] = true
		} else {
			decisions[ref] = DeletionDecision{Status: DeletionExcluded, Blocker: -1}
		}
	}

	for {
		dropBlocked(g, deletable, decisions)
		components := stronglyConnected(g, deletable)
		cyclic := false
		for _, component := range components {
			if len(component) < 2 || singleEntity(component) {
				continue
			}
			cyclic = true
			members := make(map[MarkedObjectRef]bool, len(component))
			for _, ref := range component {
				members[ref] = true
			}
			for _, ref := range component {
				blocker := slices.IndexFunc(g.Incoming[ref], func(edge DeletionEdge) bool {
					return !edge.Live && edge.Source != ref && members[edge.Source]
				})
				decisions[ref] = DeletionDecision{Status: DeletionBlocked, Reason: BlockCycle, Blocker: blocker}
				delete(deletable, ref)
			}
		}
		if !cyclic {
			break
		}
	}

	for ref := range deletable {
		status := DeletionReady
		for _, edge := range g.Incoming[ref] {
			if edge.Source != ref {
				status = DeletionWithGroup
				break
			}
		}
		decisions[ref] = DeletionDecision{Status: status, Blocker: -1}
	}

	return DeletionSchedule{Decisions: decisions, Groups: scheduleGroups(g, deletable)}
}

// dropBlocked removes objects with a reference from outside the deletable
// set until the set is stable. Removing one object can block the objects it
// references, hence the loop.
func dropBlocked(g DeletionGraph, deletable map[MarkedObjectRef]bool, decisions map[MarkedObjectRef]DeletionDecision) {
	for changed := true; changed; {
		changed = false
		for _, ref := range g.Marked {
			if !deletable[ref] {
				continue
			}
			reason, blocker := strongestBlocker(g, ref, deletable)
			if blocker < 0 {
				continue
			}
			decisions[ref] = DeletionDecision{Status: DeletionBlocked, Reason: reason, Blocker: blocker}
			delete(deletable, ref)
			changed = true
		}
	}
}

// strongestBlocker reports the most fundamental reason that blocks ref: a
// live reference first, then an unselected marked source, then a selected
// source that is blocked itself.
func strongestBlocker(g DeletionGraph, ref MarkedObjectRef, deletable map[MarkedObjectRef]bool) (BlockReason, int) {
	rank := map[BlockReason]int{BlockLiveReference: 0, BlockNotSelected: 1, BlockChain: 2}
	bestReason, best := BlockReason(""), -1
	for i, edge := range g.Incoming[ref] {
		if edge.Source == ref {
			continue // an object never blocks its own deletion
		}
		var reason BlockReason
		switch {
		case edge.Live:
			reason = BlockLiveReference
		case !g.Selected[edge.Source]:
			reason = BlockNotSelected
		case !deletable[edge.Source]:
			reason = BlockChain
		default:
			continue
		}
		if best < 0 || rank[reason] < rank[bestReason] {
			bestReason, best = reason, i
		}
	}
	return bestReason, best
}

// edgesWithin returns, per deletable object, the deletable objects that
// reference it, in Incoming order and without duplicates or self-loops.
func edgesWithin(g DeletionGraph, deletable map[MarkedObjectRef]bool) map[MarkedObjectRef][]MarkedObjectRef {
	sources := make(map[MarkedObjectRef][]MarkedObjectRef, len(deletable))
	for _, ref := range g.Marked {
		if !deletable[ref] {
			continue
		}
		for _, edge := range g.Incoming[ref] {
			if edge.Live || edge.Source == ref || !deletable[edge.Source] || slices.Contains(sources[ref], edge.Source) {
				continue
			}
			sources[ref] = append(sources[ref], edge.Source)
		}
	}
	return sources
}

// stronglyConnected returns the strongly connected components of the
// deletable subgraph (Tarjan), each in Marked order.
func stronglyConnected(g DeletionGraph, deletable map[MarkedObjectRef]bool) [][]MarkedObjectRef {
	sources := edgesWithin(g, deletable)
	// Successors: an edge runs from the referencing object to the referenced one.
	successors := make(map[MarkedObjectRef][]MarkedObjectRef, len(sources))
	for _, target := range g.Marked {
		for _, source := range sources[target] {
			successors[source] = append(successors[source], target)
		}
	}
	order := markedOrder(g)
	index := make(map[MarkedObjectRef]int, len(deletable))
	low := make(map[MarkedObjectRef]int, len(deletable))
	onStack := make(map[MarkedObjectRef]bool, len(deletable))
	var stack []MarkedObjectRef
	var components [][]MarkedObjectRef
	next := 0

	var visit func(ref MarkedObjectRef)
	visit = func(ref MarkedObjectRef) {
		index[ref], low[ref] = next, next
		next++
		stack = append(stack, ref)
		onStack[ref] = true
		for _, successor := range successors[ref] {
			if _, seen := index[successor]; !seen {
				visit(successor)
				low[ref] = min(low[ref], low[successor])
			} else if onStack[successor] {
				low[ref] = min(low[ref], index[successor])
			}
		}
		if low[ref] != index[ref] {
			return
		}
		var component []MarkedObjectRef
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			component = append(component, top)
			if top == ref {
				break
			}
		}
		slices.SortFunc(component, func(a, b MarkedObjectRef) int { return order[a] - order[b] })
		components = append(components, component)
	}
	for _, ref := range g.Marked {
		if _, seen := index[ref]; deletable[ref] && !seen {
			visit(ref)
		}
	}
	return components
}

// scheduleGroups splits the deletable set into independent groups and orders
// each group into layers (Kahn's algorithm over strongly connected components).
func scheduleGroups(g DeletionGraph, deletable map[MarkedObjectRef]bool) [][][]MarkedObjectRef {
	if len(deletable) == 0 {
		return nil
	}
	order := markedOrder(g)
	sources := edgesWithin(g, deletable)
	components := stronglyConnected(g, deletable)
	componentOf := make(map[MarkedObjectRef]int, len(deletable))
	for i, component := range components {
		for _, ref := range component {
			componentOf[ref] = i
		}
	}

	// Union-find over references gives the independent groups.
	parent := make([]int, len(components))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	indegree := make([]int, len(components))
	successors := make([][]int, len(components))
	for target, list := range sources {
		for _, source := range list {
			from, to := componentOf[source], componentOf[target]
			if from == to || slices.Contains(successors[from], to) {
				continue
			}
			successors[from] = append(successors[from], to)
			indegree[to]++
			parent[find(from)] = find(to)
		}
	}

	byRoot := make(map[int][]int)
	var roots []int
	for i := range components {
		root := find(i)
		if _, ok := byRoot[root]; !ok {
			roots = append(roots, root)
		}
		byRoot[root] = append(byRoot[root], i)
	}
	first := func(componentIDs []int) int {
		best := len(order)
		for _, c := range componentIDs {
			best = min(best, order[components[c][0]])
		}
		return best
	}
	slices.SortFunc(roots, func(a, b int) int { return first(byRoot[a]) - first(byRoot[b]) })

	groups := make([][][]MarkedObjectRef, 0, len(roots))
	for _, root := range roots {
		ready := make([]int, 0)
		for _, c := range byRoot[root] {
			if indegree[c] == 0 {
				ready = append(ready, c)
			}
		}
		var layers [][]MarkedObjectRef
		for len(ready) > 0 {
			var layer []MarkedObjectRef
			var nextReady []int
			for _, c := range ready {
				layer = append(layer, components[c]...)
				for _, successor := range successors[c] {
					indegree[successor]--
					if indegree[successor] == 0 {
						nextReady = append(nextReady, successor)
					}
				}
			}
			slices.SortFunc(layer, func(a, b MarkedObjectRef) int { return order[a] - order[b] })
			layers = append(layers, layer)
			ready = nextReady
		}
		groups = append(groups, layers)
	}
	return groups
}

func markedOrder(g DeletionGraph) map[MarkedObjectRef]int {
	order := make(map[MarkedObjectRef]int, len(g.Marked))
	for i, ref := range g.Marked {
		order[ref] = i
	}
	return order
}

func singleEntity(refs []MarkedObjectRef) bool {
	for _, ref := range refs[1:] {
		if ref.EntityName != refs[0].EntityName {
			return false
		}
	}
	return true
}

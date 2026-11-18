package domain

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/id"
)

func ref(entity string) MarkedObjectRef { return MarkedObjectRef{EntityName: entity, EntityID: id.New()} }

func selectAll(refs ...MarkedObjectRef) map[MarkedObjectRef]bool {
	selected := make(map[MarkedObjectRef]bool, len(refs))
	for _, r := range refs {
		selected[r] = true
	}
	return selected
}

func from(source MarkedObjectRef) DeletionEdge { return DeletionEdge{Source: source} }

func live(source MarkedObjectRef) DeletionEdge { return DeletionEdge{Source: source, Live: true} }

func TestPlanBlocksObjectReferencedByLiveObject(t *testing.T) {
	counterparty, contract := ref("Counterparty"), ref("Contract")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{counterparty},
		Selected: selectAll(counterparty),
		Incoming: map[MarkedObjectRef][]DeletionEdge{counterparty: {live(contract)}},
	})
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockLiveReference, Blocker: 0}, plan.Decisions[counterparty])
	require.Empty(t, plan.Groups)
}

func TestPlanDeletesMarkedChainTogetherReferencingObjectsFirst(t *testing.T) {
	counterparty, contract := ref("Counterparty"), ref("Contract")
	// The referenced object comes first: the order must not depend on the scan.
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{counterparty, contract},
		Selected: selectAll(counterparty, contract),
		Incoming: map[MarkedObjectRef][]DeletionEdge{counterparty: {from(contract)}},
	})
	require.Equal(t, DeletionWithGroup, plan.Decisions[counterparty].Status)
	require.Equal(t, DeletionReady, plan.Decisions[contract].Status)
	require.Equal(t, [][][]MarkedObjectRef{{{contract}, {counterparty}}}, plan.Groups)
}

func TestPlanBlocksObjectReferencedByUnselectedMarkedObject(t *testing.T) {
	counterparty, contract := ref("Counterparty"), ref("Contract")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{counterparty, contract},
		Selected: selectAll(counterparty),
		Incoming: map[MarkedObjectRef][]DeletionEdge{counterparty: {from(contract)}},
	})
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockNotSelected, Blocker: 0}, plan.Decisions[counterparty])
	require.Equal(t, DeletionExcluded, plan.Decisions[contract].Status)
	require.Empty(t, plan.Groups)
}

func TestPlanPropagatesBlockThroughChain(t *testing.T) {
	counterparty, contract, document := ref("Counterparty"), ref("Contract"), ref("Invoice")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{counterparty, contract},
		Selected: selectAll(counterparty, contract),
		Incoming: map[MarkedObjectRef][]DeletionEdge{
			counterparty: {from(contract)},
			contract:     {live(document)},
		},
	})
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockLiveReference, Blocker: 0}, plan.Decisions[contract])
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockChain, Blocker: 0}, plan.Decisions[counterparty])
}

func TestPlanPrefersMostFundamentalReason(t *testing.T) {
	target, blocked, other, liveSource := ref("Counterparty"), ref("Contract"), ref("Contract"), ref("Invoice")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{target, blocked, other},
		Selected: selectAll(target, blocked),
		Incoming: map[MarkedObjectRef][]DeletionEdge{
			target:  {from(blocked), from(other), live(liveSource)},
			blocked: {live(liveSource)},
		},
	})
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockLiveReference, Blocker: 2}, plan.Decisions[target])
}

func TestPlanIgnoresSelfReference(t *testing.T) {
	group := ref("Counterparty")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{group},
		Selected: selectAll(group),
		Incoming: map[MarkedObjectRef][]DeletionEdge{group: {from(group)}},
	})
	require.Equal(t, DeletionReady, plan.Decisions[group].Status)
	require.Equal(t, [][][]MarkedObjectRef{{{group}}}, plan.Groups)
}

func TestPlanDeletesHierarchyChildrenBeforeParents(t *testing.T) {
	root, child, grandchild := ref("Counterparty"), ref("Counterparty"), ref("Counterparty")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{root, child, grandchild},
		Selected: selectAll(root, child, grandchild),
		Incoming: map[MarkedObjectRef][]DeletionEdge{
			root:  {from(child)},
			child: {from(grandchild)},
		},
	})
	require.Equal(t, [][][]MarkedObjectRef{{{grandchild}, {child}, {root}}}, plan.Groups)
}

func TestPlanDeletesSameTableCycleInOneLayer(t *testing.T) {
	a, b, referenced := ref("Counterparty"), ref("Counterparty"), ref("Currency")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{referenced, a, b},
		Selected: selectAll(referenced, a, b),
		Incoming: map[MarkedObjectRef][]DeletionEdge{
			a:          {from(b)},
			b:          {from(a)},
			referenced: {from(a)},
		},
	})
	require.Equal(t, DeletionWithGroup, plan.Decisions[a].Status)
	require.Equal(t, [][][]MarkedObjectRef{{{a, b}, {referenced}}}, plan.Groups)
}

func TestPlanBlocksCrossTableCycleAndWhatItReferences(t *testing.T) {
	x, y, z := ref("Contract"), ref("Counterparty"), ref("Currency")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{x, y, z},
		Selected: selectAll(x, y, z),
		Incoming: map[MarkedObjectRef][]DeletionEdge{
			x: {from(y)},
			y: {from(x)},
			z: {from(x)},
		},
	})
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockCycle, Blocker: 0}, plan.Decisions[x])
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockCycle, Blocker: 0}, plan.Decisions[y])
	require.Equal(t, DeletionDecision{Status: DeletionBlocked, Reason: BlockChain, Blocker: 0}, plan.Decisions[z])
	require.Empty(t, plan.Groups)
}

func TestPlanSplitsIndependentGroups(t *testing.T) {
	cp1, ct1, cp2, ct2, wallet := ref("Counterparty"), ref("Contract"), ref("Counterparty"), ref("Contract"), ref("Wallet")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{cp1, cp2, ct1, ct2, wallet},
		Selected: selectAll(cp1, cp2, ct1, ct2, wallet),
		Incoming: map[MarkedObjectRef][]DeletionEdge{
			cp1: {from(ct1)},
			cp2: {from(ct2)},
		},
	})
	require.Equal(t, [][][]MarkedObjectRef{
		{{ct1}, {cp1}},
		{{ct2}, {cp2}},
		{{wallet}},
	}, plan.Groups)
}

func TestPlanDiamondWaitsForEveryReferencingObject(t *testing.T) {
	top, left, right, bottom := ref("Invoice"), ref("Contract"), ref("Contract"), ref("Counterparty")
	plan := PlanMarkedDeletion(DeletionGraph{
		Marked:   []MarkedObjectRef{bottom, right, left, top},
		Selected: selectAll(top, left, right, bottom),
		Incoming: map[MarkedObjectRef][]DeletionEdge{
			left:   {from(top)},
			right:  {from(top)},
			bottom: {from(left), from(right), from(top)},
		},
	})
	require.Equal(t, [][][]MarkedObjectRef{{{top}, {right, left}, {bottom}}}, plan.Groups)
}

func TestPlanDecisionsDoNotDependOnScanOrder(t *testing.T) {
	nodes := make([]MarkedObjectRef, 0, 12)
	for _, entity := range []string{"Counterparty", "Contract", "Invoice", "Currency"} {
		for range 3 {
			nodes = append(nodes, ref(entity))
		}
	}
	random := rand.New(rand.NewPCG(1, 2))
	incoming := make(map[MarkedObjectRef][]DeletionEdge)
	for i, target := range nodes {
		for _, source := range nodes[i+1:] {
			if random.IntN(4) == 0 {
				incoming[target] = append(incoming[target], from(source))
			}
		}
		if random.IntN(6) == 0 {
			incoming[target] = append(incoming[target], live(ref("Invoice")))
		}
	}
	selected := selectAll(nodes...)
	delete(selected, nodes[len(nodes)-1])
	want := PlanMarkedDeletion(DeletionGraph{Marked: nodes, Selected: selected, Incoming: incoming}).Decisions
	for range 20 {
		shuffled := slicesClone(nodes)
		random.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got := PlanMarkedDeletion(DeletionGraph{Marked: shuffled, Selected: selected, Incoming: incoming})
		for _, node := range nodes {
			require.Equal(t, want[node].Status, got.Decisions[node].Status)
		}
		requireReferencingFirst(t, got, incoming)
	}
}

// requireReferencingFirst checks that every deletable object is deleted in a
// later layer than any deletable object of another table that references it.
func requireReferencingFirst(t *testing.T, plan DeletionSchedule, incoming map[MarkedObjectRef][]DeletionEdge) {
	t.Helper()
	position := map[MarkedObjectRef][2]int{}
	for g, group := range plan.Groups {
		for l, layer := range group {
			for _, r := range layer {
				position[r] = [2]int{g, l}
			}
		}
	}
	for target, edges := range incoming {
		at, ok := position[target]
		if !ok {
			continue
		}
		for _, edge := range edges {
			source, ok := position[edge.Source]
			if !ok || edge.Source == target {
				continue
			}
			require.Equal(t, at[0], source[0], "referencing objects share the group")
			require.Less(t, source[1], at[1], "referencing objects go first")
		}
	}
}

func slicesClone(refs []MarkedObjectRef) []MarkedObjectRef { return append([]MarkedObjectRef(nil), refs...) }

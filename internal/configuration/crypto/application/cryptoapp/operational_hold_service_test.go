package cryptoapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"
)

type operationalHoldRepoStub struct {
	acquired []AcquireOperationalHoldCommand
	owner    string
	resolved int
}

func (r *operationalHoldRepoStub) AcquireOperationalHold(_ context.Context, command AcquireOperationalHoldCommand) error {
	r.acquired = append(r.acquired, command)
	return nil
}

func (r *operationalHoldRepoStub) ResolveOperationalHolds(_ context.Context, ownerType, ownerID, _, _ string) (int64, error) {
	r.owner = ownerType + ":" + ownerID
	r.resolved++
	return 2, nil
}

func TestOperationalHoldServiceUsesImmutableOwnerBoundary(t *testing.T) {
	repo := &operationalHoldRepoStub{}
	service := NewOperationalHoldService(&immediateTxManager{}, repo)
	ownerID := id.New().String()

	require.NoError(t, service.Acquire(context.Background(), AcquireOperationalHoldCommand{
		Scope: OperationalHoldScopeNetwork, ScopeID: id.New(), Reason: "external_outflow",
		OwnerType: "external_outflow", OwnerID: ownerID, HeldBy: "chain-watcher",
	}))
	require.Len(t, repo.acquired, 1)
	require.Equal(t, "external_outflow", repo.acquired[0].OwnerType)
	require.NotNil(t, repo.acquired[0].Details)

	require.NoError(t, service.ResolveOwner(context.Background(), "external_outflow", ownerID, id.New().String(), "review completed"))
	require.Equal(t, "external_outflow:"+ownerID, repo.owner)
	require.Equal(t, 1, repo.resolved)
}

func TestOperationalHoldServiceRejectsScopeWideRelease(t *testing.T) {
	repo := &operationalHoldRepoStub{}
	service := NewOperationalHoldService(&immediateTxManager{}, repo)

	require.Error(t, service.ResolveOwner(context.Background(), "", "", "system", "release network"))
	require.Zero(t, repo.resolved)
	require.Error(t, service.Acquire(context.Background(), AcquireOperationalHoldCommand{
		Scope: OperationalHoldScopeNetwork, ScopeID: id.New(), Reason: "deep_reorg", HeldBy: "watcher",
	}))
	require.Empty(t, repo.acquired)
}

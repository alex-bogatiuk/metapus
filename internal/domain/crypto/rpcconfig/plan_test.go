package rpcconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
)

func planFixture() RPCConfiguration {
	return RPCConfiguration{NetworkID: id.New(), Source: "database", Revision: strings.Repeat("a", 64),
		Endpoints: []RPCConfigurationEndpoint{
			{ID: id.New(), Version: 1, Role: "rpc", TrustGroup: "operator-one", IsActive: true, Priority: 10},
			{ID: id.New(), Version: 2, Role: "rpc", TrustGroup: "operator-two", IsActive: true, Priority: 20},
			{ID: id.New(), Version: 3, Role: "rpc", TrustGroup: "operator-three", IsActive: false, Priority: 30},
		}, Requirements: []RPCRoleRequirement{{Role: "rpc", RequiredProviders: 2, ConfiguredProviders: 2}},
	}
}

func TestRPCPlanValidatesFinalReplacementWithoutMutatingSnapshot(t *testing.T) {
	c := planFixture()
	first, reserve := c.Endpoints[0], c.Endpoints[2]
	changes, err := c.Plan(RPCConfigurationUpdate{ExpectedRevision: c.Revision, Endpoints: []RPCConfigurationChange{
		{ID: first.ID, Version: first.Version, IsActive: false, Priority: 10},
		{ID: reserve.ID, Version: reserve.Version, IsActive: true, Priority: 30},
	}})
	require.NoError(t, err)
	require.Len(t, changes, 2)
	require.True(t, c.Endpoints[0].IsActive)
	require.False(t, c.Endpoints[2].IsActive)
	require.Equal(t, 2, c.Requirements[0].ConfiguredProviders)
}

func TestRPCPlanCountsOperatorsInsteadOfEndpoints(t *testing.T) {
	c := planFixture()
	c.Endpoints[2].TrustGroup = c.Endpoints[1].TrustGroup
	_, err := c.Plan(RPCConfigurationUpdate{ExpectedRevision: c.Revision, Endpoints: []RPCConfigurationChange{
		{ID: c.Endpoints[0].ID, Version: 1, IsActive: false, Priority: 10},
		{ID: c.Endpoints[2].ID, Version: 3, IsActive: true, Priority: 30},
	}})
	require.ErrorContains(t, err, "независимых")
}

func TestRPCPlanNormalizesOnlyTheReorderedRoleIncludingDisabledRows(t *testing.T) {
	c := planFixture()
	c.Endpoints = append(c.Endpoints, RPCConfigurationEndpoint{ID: id.New(), Version: 1, Role: "indexer", TrustGroup: "indexer", IsActive: true, Priority: 7})
	changes, err := c.Plan(RPCConfigurationUpdate{ExpectedRevision: c.Revision, Endpoints: []RPCConfigurationChange{
		{ID: c.Endpoints[2].ID, Version: 3, IsActive: false, Priority: 5},
	}})
	require.NoError(t, err)
	require.Len(t, changes, 3)
	require.Equal(t, c.Endpoints[2].ID, changes[0].ID)
	require.Equal(t, 10, changes[0].Priority)
	require.False(t, changes[0].IsActive)
	require.Equal(t, 20, changes[1].Priority)
	require.Equal(t, 30, changes[2].Priority)
	for _, change := range changes {
		require.NotEqual(t, c.Endpoints[3].ID, change.ID)
	}
}

func TestRPCPlanRejectsStaleSnapshotRowsAndUnknownIDs(t *testing.T) {
	for _, kind := range []string{"snapshot", "row", "foreign", "file"} {
		t.Run(kind, func(t *testing.T) {
			c := planFixture()
			u := RPCConfigurationUpdate{ExpectedRevision: c.Revision, Endpoints: []RPCConfigurationChange{{ID: c.Endpoints[0].ID, Version: 1, IsActive: true, Priority: 10}}}
			switch kind {
			case "snapshot":
				u.ExpectedRevision = strings.Repeat("b", 64)
			case "row":
				u.Endpoints[0].Version++
			case "foreign":
				u.Endpoints[0].ID = id.New()
			case "file":
				c.Source = "file"
			}
			_, err := c.Plan(u)
			require.Error(t, err)
		})
	}
	c := planFixture()
	changes, err := c.Plan(RPCConfigurationUpdate{ExpectedRevision: c.Revision})
	require.NoError(t, err)
	require.Empty(t, changes)
}

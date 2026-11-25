package rpcconfig

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

func TestResolveRPCTrustGroupTrustsTheKnownHostOverTheForm(t *testing.T) {
	for _, tt := range []struct{ url, configured, want string }{
		{"https://tron-nile.core.chainstack.com", "", "chainstack"},
		{"https://tron-nile.core.chainstack.com", "independent", "chainstack"},
		{"https://shared.eu-central-1.getblock.io", "", "getblock"},
		{"https://nile.trongrid.io", "", "trongrid"},
		{"https://rpc.example.test", " own-node ", "own-node"},
		{"https://rpc.example.test", "", ""},
		{"not a url", "", ""},
	} {
		t.Run(tt.url+"/"+tt.configured, func(t *testing.T) {
			require.Equal(t, tt.want, ResolveRPCTrustGroup(tt.url, tt.configured))
		})
	}
}

func TestRPCEndpointNeedsAnOperatorOnlyForAnUnknownHost(t *testing.T) {
	e := NewRPCEndpoint("RPC-1", "Chainstack Nile FullNode")
	e.NetworkID, e.Role, e.URL = id.New(), RoleFullNode, "https://tron-nile.core.chainstack.com"
	require.NoError(t, e.Validate(context.Background()))

	e.URL = "https://rpc.example.test"
	require.Error(t, e.Validate(context.Background()))
	e.TrustGroup = "own-node"
	require.NoError(t, e.Validate(context.Background()))

	e.Role = "archive"
	require.Error(t, e.Validate(context.Background()))
}

func TestAssignSourceKeepsAnExistingSourceIdentifier(t *testing.T) {
	e := NewRPCEndpoint("RPC-7", "Chainstack Nile Solidity")
	e.URL, e.TrustGroup = "https://tron-nile.core.chainstack.com", "typed-by-hand"
	e.AssignSource()
	require.Equal(t, "RPC-7", e.ProviderID)
	require.Equal(t, "chainstack", e.TrustGroup)

	e.Code, e.ProviderID = "RPC-8", "chainstack-nile"
	e.AssignSource()
	require.Equal(t, "chainstack-nile", e.ProviderID, "evidence keeps naming the same source")
}

func TestRolesForFamily(t *testing.T) {
	require.Equal(t, []Role{RoleRPC}, RolesForFamily("evm"))
	require.Equal(t, []Role{RoleFullNode, RoleSolidity, RoleIndexer}, RolesForFamily("tron"))
	require.Empty(t, RolesForFamily("ton"))
	for _, family := range []string{"evm", "tron"} {
		for _, role := range RolesForFamily(family) {
			require.True(t, role.Valid())
		}
	}
}

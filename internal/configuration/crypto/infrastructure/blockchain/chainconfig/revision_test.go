package chainconfig

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

func TestRPCRevisionIsDeterministicAndExcludesLocalTransports(t *testing.T) {
	network := Network{Roles: map[Role][]Endpoint{
		RoleFullNode: {{EndpointID: id.New(), ProviderID: "one", URL: "https://one.example.test", Priority: 10}},
		RoleSolidity: {{EndpointID: id.New(), ProviderID: "two", URL: "https://two.example.test", Priority: 10}},
	}}
	want := network.Revision("tron", 2)
	for range 30 {
		require.Equal(t, want, network.Revision("tron", 2))
	}
	network.Roles[RoleFullNode][0].AdmissionScope = "local-process"
	require.Equal(t, want, network.Revision("tron", 2))
	network.Roles[RoleFullNode][0].ConfigVersion++
	require.NotEqual(t, want, network.Revision("tron", 2))
}

func TestNeutralTopologyPreservesLegacyRevisionEncoding(t *testing.T) {
	for _, network := range []Network{
		{}, {Roles: map[Role][]Endpoint{}}, {Roles: map[Role][]Endpoint{RoleRPC: nil, RoleIndexer: {}}},
		{Roles: map[Role][]Endpoint{RoleRPC: {{EndpointID: id.New(), ConfigVersion: 3, ProviderID: "one", TrustGroup: "operator", Priority: 10, URL: "https://fixture.test", MinRequestIntervalMS: 200, DisableKeepAlives: true, APIKeySecretFile: "secret-reference", APIKey: "never-serialized"}}}, EVM: &EVMSettings{MaxAttempts: 3}, TRON: &TRONSettings{IngestionMode: "hybrid"}},
	} {
		legacy, err := json.MarshalDeterministic(network)
		require.NoError(t, err)
		neutral, err := json.MarshalDeterministic(network.Configuration())
		require.NoError(t, err)
		require.Equal(t, string(legacy), string(neutral))
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(legacy)), network.Revision("evm", 2))
	}
}

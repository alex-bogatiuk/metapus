package cryptodto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/pkg/sdk/id"
)

func TestUpdateCryptoNetworkPolicyMapsExternalResources(t *testing.T) {
	req := UpdateCryptoNetworkPolicyRequest{
		Version: 1,
		ExternalResources: &ExternalResourcePolicyRequest{
			Strategy: "prefer_rental", ProviderAccountIDs: []string{id.New().String()}, ProviderFailureThreshold: 3, ProviderOpenSeconds: 60,
		},
	}
	patch, err := req.ToDomain()
	require.NoError(t, err)
	require.NotNil(t, patch.ExternalResources)
	require.Equal(t, "prefer_rental", string(patch.ExternalResources.Strategy))
	require.Len(t, patch.ExternalResources.ProviderAccountIDs, 1)
	require.False(t, patch.ClearExternalResources)

	req.ExternalResources = nil
	req.ClearExternalResources = true
	patch, err = req.ToDomain()
	require.NoError(t, err)
	require.Nil(t, patch.ExternalResources)
	require.True(t, patch.ClearExternalResources)
}

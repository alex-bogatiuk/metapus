package rpcconfig

import (
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/integrations"
)

func TestRPCKeysOfAPool(t *testing.T) {
	accounts := []integrations.AccountKeys{
		{Name: "a", HasCredentials: true, Keys: []string{"primary", "archive"}},
		{Name: "b", HasCredentials: true, Keys: []string{"archive"}},
		{Name: "unconfigured"},
	}
	require.Equal(t, []string{"archive"}, CommonRPCKeyNames(accounts))
	require.Empty(t, CommonRPCKeyNames(accounts[2:]))
	require.NoError(t, RequireRPCKeys([]RPCKeyUser{{Endpoint: "e", Key: "archive"}}, accounts))
	err := RequireRPCKeys([]RPCKeyUser{{Endpoint: "e", Key: "primary"}}, accounts)
	require.ErrorContains(t, err, "«b»")
	require.NotContains(t, err.Error(), "unconfigured")
}

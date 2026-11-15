package providerpolicy

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSecretFreeRPCURLsKeepTenantPoolsAndGenerationsIndependent(t *testing.T) {
	c := Capacity{Concurrency: 8, BulkConcurrency: 4, RequestsPerSecond: 10}
	a, err := AcquireScoped("tenant-a/pool-a", "https://eth-sepolia.g.alchemy.com/v2/", c)
	require.NoError(t, err)
	defer Release(a)
	same, err := AcquireScoped("tenant-a/pool-a", "https://eth-sepolia.g.alchemy.com/v2/", c)
	require.NoError(t, err)
	defer Release(same)
	require.Same(t, a, same)
	other, err := AcquireScoped("tenant-b/pool-b", "https://eth-sepolia.g.alchemy.com/v2/", c)
	require.NoError(t, err)
	defer Release(other)
	require.NotSame(t, a, other)
	c.MinRequestIntervalMS = 30000
	next, err := AcquireScoped("tenant-a/pool-a", "https://eth-sepolia.g.alchemy.com/v2/", c)
	require.NoError(t, err)
	defer Release(next)
	require.NotSame(t, a, next)
}

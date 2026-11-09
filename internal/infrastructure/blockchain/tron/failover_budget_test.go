package tron

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/infrastructure/blockchain/chainconfig"
	"metapus/internal/infrastructure/blockchain/rpcpolicy"
)

func TestQuorumTimeoutPreservesBudgetForIndependentReserve(t *testing.T) {
	epoch := time.Now().UnixMilli()
	a, b, c := newSimulatedRPC(t, epoch), newSimulatedRPC(t, epoch), newSimulatedRPC(t, epoch)
	b.fault.Store(simTimeout)
	endpoints := []chainconfig.Endpoint{
		{ProviderID: "healthy-a", TrustGroup: "a", URL: a.server.URL},
		{ProviderID: "hanging-b", TrustGroup: "b", URL: b.server.URL},
		{ProviderID: "healthy-c", TrustGroup: "c", URL: c.server.URL},
	}
	pool, err := NewRPCPool(RPCPoolConfig{NetworkCode: "TEST", FailureThreshold: 3, FullNode: endpoints, Solidity: endpoints,
		Settings: chainconfig.TRONSettings{AttemptTimeoutMS: 200, ReadBudgetMS: 400, MaxAttempts: 2, RequestsPerSecond: 1000}})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, sources, err := pool.ReadAgreedBlock(t.Context(), 151)
	require.NoError(t, err, "two timeouts at the same provider used to consume the entire 400 ms budget")
	require.Len(t, sources, 2)
	require.NotEqual(t, sources[0].TrustGroup, sources[1].TrustGroup)
	for _, source := range sources {
		require.NotEqual(t, "hanging-b", source.ProviderID)
	}
	require.Positive(t, c.receipts.Load(), "the complete reserve block must be read and verified")
}

func TestExpiredTRONCircuitsCannotHideHealthyThirdProvider(t *testing.T) {
	bad := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
		}))
	}
	a, b := bad(), bad()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	c := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"balance":123}`))
	}))
	t.Cleanup(c.Close)
	pool := policyPool(t, a.URL, b.URL, c.URL)
	for _, endpoint := range pool.byRole[chainconfig.RoleSolidity][:2] {
		endpoint.mu.Lock()
		endpoint.operations["GetTRXBalance"] = &rpcpolicy.Circuit{Failures: 3, OpenUntil: time.Now().Add(-time.Second), LastError: "timeout"}
		endpoint.mu.Unlock()
	}
	balance, err := pool.GetTRXBalance(t.Context(), "account")
	require.NoError(t, err)
	require.Equal(t, "123", balance.String())
}

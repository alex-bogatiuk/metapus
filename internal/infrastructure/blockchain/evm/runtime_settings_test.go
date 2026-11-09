package evm

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/infrastructure/blockchain/chainconfig"
)

func TestRPCReloadPinsClientsAndLimitsUntilPrefetchWindowFinishes(t *testing.T) {
	header := currentTestHeader()
	server := headerServer(t, header, nil, 0)
	defer server.Close()
	version := "first"
	next := RPCPoolConfig{NetworkCode: "TEST", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 3, OpenDuration: time.Second,
		Settings:  chainconfig.EVMSettings{NativeConcurrency: 2},
		Endpoints: []chainconfig.Endpoint{{ProviderID: "fixture", URL: server.URL, AdmissionScope: t.Name()}}}
	cfg := next
	cfg.InitialVersion = version
	cfg.Reload = func(context.Context) (RPCPoolConfig, string, error) { return next, version, nil }
	pool, err := NewRPCPool(t.Context(), cfg)
	require.NoError(t, err)
	defer pool.Close()
	pool.runtime.Interval = 0
	old, oldSettings, finish, err := pool.acquireReadWindow(t.Context())
	require.NoError(t, err)
	defer finish()
	version = "second"
	next.Settings = chainconfig.EVMSettings{NativeConcurrency: 6, ProviderConcurrency: 12, BackfillConcurrency: 6, RequestsPerSecond: 30, AttemptTimeoutMS: 3000, ReadBudgetMS: 8000}
	current, currentSettings, release, err := pool.acquireReadWindow(t.Context())
	require.NoError(t, err)
	defer release()
	require.NotSame(t, old, current)
	require.Equal(t, 2, oldSettings.NativeConcurrency)
	require.Equal(t, 6, currentSettings.NativeConcurrency)
	require.Equal(t, 12, currentSettings.ProviderConcurrency)
	require.Equal(t, 3000, currentSettings.AttemptTimeoutMS)
	_, err = old.HeaderByNumber(t.Context(), header.Number)
	require.NoError(t, err, "retired clients stay usable until the pinned window drains")
	require.Equal(t, version, pool.Status()[0].AppliedRevision)
}

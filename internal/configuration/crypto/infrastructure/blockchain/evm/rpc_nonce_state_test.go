package evm

import (
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/json"
)

// nonceServer answers the head and nonce reads of ObserveNonceState. headAge
// shifts the latest block timestamp into the past to model a lagging node.
func nonceServer(t *testing.T, headAge time.Duration, nonce uint64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var value any
		switch request.Method {
		case "eth_chainId":
			value = "0xaa36a7"
		case "eth_getTransactionCount":
			value = fmt.Sprintf("0x%x", nonce)
		case "eth_getBlockByNumber":
			header := currentTestHeader()
			header.Time = uint64(time.Now().Add(-headAge).Unix())
			raw, err := json.Marshal(header)
			if err != nil {
				t.Error(err)
				return
			}
			value = json.RawMessage(raw)
		default:
			t.Errorf("unexpected method %s", request.Method)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": value})
	}))
}

func noncePool(t *testing.T, servers map[string]*httptest.Server) *RPCPool {
	t.Helper()
	endpoints := make([]chainconfig.Endpoint, 0, len(servers))
	priority := 0
	for provider, server := range servers {
		endpoints = append(endpoints, chainconfig.Endpoint{ProviderID: provider, TrustGroup: provider, URL: server.URL, Priority: priority})
		priority++
	}
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 3, OpenDuration: time.Minute, Endpoints: endpoints})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// A frozen provider reports nonce 0 for a source the chain has advanced to 3.
// Its stale head excludes it, so healthy providers decide without divergence.
func TestObserveNonceStateExcludesProviderWithStaleHead(t *testing.T) {
	frozen := nonceServer(t, 20*time.Hour, 0)
	defer frozen.Close()
	first := nonceServer(t, 0, 3)
	defer first.Close()
	second := nonceServer(t, 0, 3)
	defer second.Close()
	pool := noncePool(t, map[string]*httptest.Server{"frozen": frozen, "first": first, "second": second})

	snapshot, err := pool.ObserveNonceState(t.Context(), "0x3d111ae105EbBA77c10F6542Fe3736C405067D47")
	require.NoError(t, err)
	require.False(t, snapshot.Divergent)
	require.Equal(t, uint64(3), snapshot.Latest)
	require.Equal(t, uint64(3), snapshot.Pending)
	require.Equal(t, 2, snapshot.Observations)
}

// Without two fresh independent providers the state is unavailable, which
// callers retry, rather than a divergence that holds the source.
func TestObserveNonceStateWithoutFreshQuorumIsUnavailableNotDivergent(t *testing.T) {
	frozen := nonceServer(t, 20*time.Hour, 0)
	defer frozen.Close()
	fresh := nonceServer(t, 0, 3)
	defer fresh.Close()
	pool := noncePool(t, map[string]*httptest.Server{"frozen": frozen, "fresh": fresh})

	snapshot, err := pool.ObserveNonceState(t.Context(), "0x3d111ae105EbBA77c10F6542Fe3736C405067D47")
	require.ErrorContains(t, err, "required providers returned nonce state")
	require.False(t, snapshot.Divergent)
}

// Fresh providers that really disagree remain a divergence.
func TestObserveNonceStateKeepsDivergenceBetweenFreshProviders(t *testing.T) {
	first := nonceServer(t, 0, 3)
	defer first.Close()
	second := nonceServer(t, 0, 2)
	defer second.Close()
	pool := noncePool(t, map[string]*httptest.Server{"first": first, "second": second})

	snapshot, err := pool.ObserveNonceState(t.Context(), "0x3d111ae105EbBA77c10F6542Fe3736C405067D47")
	require.NoError(t, err)
	require.True(t, snapshot.Divergent)
}

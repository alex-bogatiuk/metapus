package evm

import (
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/json"
)

// quorumServer serves header for "latest" at once and answers historical
// reads after delay, or with a JSON-RPC error when fail is set. It counts the
// historical reads, which are the quorum's.
func quorumServer(t *testing.T, header *types.Header, delay time.Duration, fail bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var historical atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var value any = "0xaa36a7"
		if request.Method != "eth_chainId" {
			if string(request.Params[0]) != `"latest"` {
				historical.Add(1)
				if fail {
					_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32000,"message":"unavailable"}}`, request.ID)
					return
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(delay):
				}
			}
			value = header
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": value})
	})), &historical
}

func newQuorumPool(t *testing.T, urls ...string) *RPCPool {
	t.Helper()
	endpoints := make([]chainconfig.Endpoint, 0, len(urls))
	for i, url := range urls {
		endpoints = append(endpoints, chainconfig.Endpoint{ProviderID: fmt.Sprintf("provider-%d", i), URL: url, Priority: i})
	}
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 10, OpenDuration: time.Second,
		Settings: chainconfig.EVMSettings{AttemptTimeoutMS: 400, ReadBudgetMS: 1000}, Endpoints: endpoints})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func TestCanonicalQuorumAsksOnlyTwoGroupsWhenTheyAnswer(t *testing.T) {
	h := currentTestHeader()
	var servers []*httptest.Server
	var counts []*atomic.Int32
	for range 4 {
		server, count := quorumServer(t, h, 0, false)
		defer server.Close()
		servers, counts = append(servers, server), append(counts, count)
	}
	pool := newQuorumPool(t, servers[0].URL, servers[1].URL, servers[2].URL, servers[3].URL)

	header, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, h.Hash(), header.Hash())
	require.Equal(t, []int32{1, 1, 0, 0}, []int32{counts[0].Load(), counts[1].Load(), counts[2].Load(), counts[3].Load()})
}

func TestCanonicalQuorumReplacesFailedProviderWithNextGroup(t *testing.T) {
	h := currentTestHeader()
	failing, failed := quorumServer(t, h, 0, true)
	defer failing.Close()
	b, bCount := quorumServer(t, h, 0, false)
	defer b.Close()
	c, cCount := quorumServer(t, h, 0, false)
	defer c.Close()
	d, dCount := quorumServer(t, h, 0, false)
	defer d.Close()
	pool := newQuorumPool(t, failing.URL, b.URL, c.URL, d.URL)

	header, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, h.Hash(), header.Hash())
	require.Equal(t, []int32{1, 1, 1, 0}, []int32{failed.Load(), bCount.Load(), cCount.Load(), dCount.Load()})
}

func TestCanonicalQuorumHedgesSlowProviderAndCancelsIt(t *testing.T) {
	h := currentTestHeader()
	slow, slowCount := quorumServer(t, h, 5*time.Second, false)
	defer slow.Close()
	b, bCount := quorumServer(t, h, 0, false)
	defer b.Close()
	c, cCount := quorumServer(t, h, 0, false)
	defer c.Close()
	d, dCount := quorumServer(t, h, 0, false)
	defer d.Close()
	pool := newQuorumPool(t, slow.URL, b.URL, c.URL, d.URL)

	started := time.Now()
	header, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, h.Hash(), header.Hash())
	require.Less(t, time.Since(started), 400*time.Millisecond, "the hedge answers before the slow attempt times out")
	require.Equal(t, []int32{1, 1, 1, 0}, []int32{slowCount.Load(), bCount.Load(), cCount.Load(), dCount.Load()}, "one hedge, not every provider")
}

func TestCanonicalQuorumComparesHedgedVote(t *testing.T) {
	h := currentTestHeader()
	other := currentTestHeader()
	other.ParentHash = common.HexToHash("0x02")
	slow, _ := quorumServer(t, h, 5*time.Second, false)
	defer slow.Close()
	b, _ := quorumServer(t, h, 0, false)
	defer b.Close()
	diverging, _ := quorumServer(t, other, 0, false)
	defer diverging.Close()
	pool := newQuorumPool(t, slow.URL, b.URL, diverging.URL)

	header, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.Nil(t, header)
	require.ErrorIs(t, err, ErrRPCProviderDivergence)
}

func TestCanonicalQuorumSingleProviderDoesNotHedgeWithinItsGroup(t *testing.T) {
	h := currentTestHeader()
	server, count := quorumServer(t, h, 150*time.Millisecond, false)
	defer server.Close()
	pool := newQuorumPool(t, server.URL)

	header, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, h.Hash(), header.Hash())
	require.Equal(t, int32(1), count.Load())
}

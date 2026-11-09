package evm

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

func headerServer(t *testing.T, header *types.Header, change func(map[string]json.RawMessage), historicalDelay time.Duration) *httptest.Server {
	t.Helper()
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
			if historicalDelay > 0 && string(request.Params[0]) != `"latest"` {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(historicalDelay):
				}
			}
			raw, err := json.Marshal(header)
			if err != nil {
				t.Error(err)
				return
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Error(err)
				return
			}
			if change != nil {
				change(fields)
			}
			value = fields
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": value})
	}))
}

func currentTestHeader() *types.Header {
	return &types.Header{Number: big.NewInt(100), Difficulty: big.NewInt(0), GasLimit: 30_000_000, Time: uint64(time.Now().Unix())}
}

func TestLatestHeadFailsOverFromStaleAndFutureProviders(t *testing.T) {
	for _, shift := range []time.Duration{-24 * time.Hour, time.Hour} {
		t.Run(shift.String(), func(t *testing.T) {
			bad := currentTestHeader()
			bad.Time = uint64(time.Now().Add(shift).Unix())
			primary := headerServer(t, bad, nil, 0)
			defer primary.Close()
			secondary := headerServer(t, currentTestHeader(), nil, 0)
			defer secondary.Close()
			pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Minute, Endpoints: []chainconfig.Endpoint{{ProviderID: "stale", URL: primary.URL}, {ProviderID: "fresh", URL: secondary.URL, Priority: 1}}})
			require.NoError(t, err)
			defer pool.Close()
			head, err := pool.HeaderByNumber(t.Context(), nil)
			require.NoError(t, err)
			require.WithinDuration(t, time.Now(), time.Unix(int64(head.Time), 0), 2*time.Second)
			require.Equal(t, "stale_head", pool.IngestionHeaderErrorForTest())
		})
	}
}

func (p *RPCPool) IngestionHeaderErrorForTest() string {
	e := p.endpoints[0]
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.operations[opHeader].LastError
}

func TestHeaderRejectsAdvertisedHashAndHeightMismatch(t *testing.T) {
	for _, field := range []string{"hash", "number"} {
		t.Run(field, func(t *testing.T) {
			h := currentTestHeader()
			server := headerServer(t, h, func(fields map[string]json.RawMessage) {
				if field == "hash" {
					fields[field], _ = json.Marshal(common.HexToHash("0x1234"))
				} else {
					fields[field] = json.RawMessage(`"0x65"`)
				}
			}, 0)
			defer server.Close()
			pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 5, OpenDuration: time.Second, Endpoints: []chainconfig.Endpoint{{ProviderID: "invalid", URL: server.URL}}})
			require.NoError(t, err)
			defer pool.Close()
			_, err = pool.HeaderByNumber(t.Context(), big.NewInt(100))
			require.ErrorContains(t, err, "invalid_header")
		})
	}
}

func TestCanonicalQuorumSurvivesTwoSlowProvidersBeforeHealthyOnes(t *testing.T) {
	h := currentTestHeader()
	a := headerServer(t, h, nil, time.Second)
	defer a.Close()
	b := headerServer(t, h, nil, time.Second)
	defer b.Close()
	c := headerServer(t, h, nil, 0)
	defer c.Close()
	d := headerServer(t, h, nil, 0)
	defer d.Close()
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Second, Settings: chainconfig.EVMSettings{AttemptTimeoutMS: 100, ReadBudgetMS: 150}, Endpoints: []chainconfig.Endpoint{{ProviderID: "slow-a", URL: a.URL}, {ProviderID: "slow-b", URL: b.URL, Priority: 1}, {ProviderID: "healthy-a", URL: c.URL, Priority: 2}, {ProviderID: "healthy-b", URL: d.URL, Priority: 3}}})
	require.NoError(t, err)
	defer pool.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	header, err := pool.CanonicalHeaderByNumber(ctx, big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, h.Hash(), header.Hash())
}

func TestSepoliaAmsterdamHeaderHashIncludesBlockAccessList(t *testing.T) {
	raw, err := os.ReadFile("testdata/sepolia-amsterdam-header.json")
	require.NoError(t, err)
	var h types.Header
	require.NoError(t, h.UnmarshalJSON(raw))
	require.NotNil(t, h.BlockAccessListHash)
	require.Equal(t, "0x71f3df7bb76e9325ca9a70cae775e3af50e0f1c13d812428740bfe97287d0588", h.Hash().Hex())

}

func TestCanonicalQuorumPreservesLimitedFallbackAndUsesItWhenNeeded(t *testing.T) {
	for _, slow := range []bool{false, true} {
		t.Run(fmt.Sprint(slow), func(t *testing.T) {
			h := currentTestHeader()
			delay := time.Duration(0)
			if slow {
				delay = time.Second
			}
			a := headerServer(t, h, nil, delay)
			defer a.Close()
			b := headerServer(t, h, nil, 0)
			defer b.Close()
			var calls atomic.Int32
			c := headerServer(t, h, func(map[string]json.RawMessage) { calls.Add(1) }, 0)
			defer c.Close()
			pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 1, OpenDuration: time.Second, Settings: chainconfig.EVMSettings{AttemptTimeoutMS: 300, ReadBudgetMS: 1000}, Endpoints: []chainconfig.Endpoint{{ProviderID: "primary-a", URL: a.URL}, {ProviderID: "primary-b", URL: b.URL}, {ProviderID: "limited", URL: c.URL, MinRequestIntervalMS: 100}}})
			require.NoError(t, err)
			defer pool.Close()
			before := calls.Load()
			header, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
			require.NoError(t, err)
			require.Equal(t, h.Hash(), header.Hash())
			if slow {
				require.Equal(t, before+1, calls.Load())
			} else {
				require.Equal(t, before, calls.Load())
			}
		})
	}
}

package evm

import (
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/internal/core/json"
)

func TestRPCPoolFailsClosedWhenAnyEndpointReportsWrongChain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		w.Header().Set("Content-Type", "application/json")
		if request.Method == "eth_chainId" {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": "0x1"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID,
			"error": map[string]any{"code": -32601, "message": "method not found"}})
	}))
	defer server.Close()

	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "ETHEREUM-SEPOLIA",
		ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second,
		Endpoints: []chainconfig.Endpoint{{ProviderID: "wrong-chain", URL: server.URL}}})
	require.Nil(t, pool)
	require.True(t, errors.Is(err, ErrWrongChainID), err)
}

func TestCanonicalHeaderByNumberRejectsProviderDivergence(t *testing.T) {
	serverA := newHeaderRPCServer(t, common.HexToHash("0x01"))
	defer serverA.Close()
	serverB := newHeaderRPCServer(t, common.HexToHash("0x02"))
	defer serverB.Close()

	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "ETHEREUM-SEPOLIA",
		ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second,
		Endpoints: []chainconfig.Endpoint{
			{ProviderID: "provider-a", URL: serverA.URL, Priority: 1},
			{ProviderID: "provider-b", URL: serverB.URL, Priority: 2},
		}})
	require.NoError(t, err)
	defer pool.Close()

	header, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.Nil(t, header)
	require.ErrorIs(t, err, ErrRPCProviderDivergence)
}

func TestRPCPoolHonorsDisableKeepAlives(t *testing.T) {
	header := &gethtypes.Header{
		UncleHash: gethtypes.EmptyUncleHash, Root: gethtypes.EmptyRootHash,
		TxHash: gethtypes.EmptyTxsHash, ReceiptHash: gethtypes.EmptyReceiptsHash,
		Difficulty: big.NewInt(0), Number: big.NewInt(100), GasLimit: 30_000_000,
		Time: uint64(time.Now().Unix()), BaseFee: big.NewInt(1),
	}
	var mu sync.Mutex
	seenConnections := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reused := seenConnections[r.RemoteAddr]
		seenConnections[r.RemoteAddr] = true
		mu.Unlock()
		if reused {
			http.Error(w, "reused connection rejected", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		result := any(header)
		if request.Method == "eth_chainId" {
			result = "0xaa36a7"
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": result,
		}))
	}))
	defer server.Close()

	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "ETHEREUM-SEPOLIA",
		ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second,
		Endpoints: []chainconfig.Endpoint{{ProviderID: "no-keepalive", URL: server.URL, DisableKeepAlives: true}}})
	require.NoError(t, err)
	defer pool.Close()

	got, err := pool.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, header.Hash(), got.Hash())
	mu.Lock()
	connectionCount := len(seenConnections)
	mu.Unlock()
	require.GreaterOrEqual(t, connectionCount, 3)
}

func newHeaderRPCServer(t *testing.T, parentHash common.Hash) *httptest.Server {
	t.Helper()
	header := &gethtypes.Header{
		ParentHash: parentHash, UncleHash: gethtypes.EmptyUncleHash, Root: gethtypes.EmptyRootHash,
		TxHash: gethtypes.EmptyTxsHash, ReceiptHash: gethtypes.EmptyReceiptsHash,
		Difficulty: big.NewInt(0), Number: big.NewInt(100), GasLimit: 30_000_000,
		Time: uint64(time.Now().Unix()), BaseFee: big.NewInt(1),
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		w.Header().Set("Content-Type", "application/json")
		result := any(header)
		if request.Method == "eth_chainId" {
			result = "0xaa36a7"
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": request.ID, "result": result,
		}))
	}))
}

func TestCanonicalHeaderCountsOneVotePerAlchemyAccountPool(t *testing.T) {
	a := newHeaderRPCServer(t, common.HexToHash("0x01"))
	defer a.Close()
	b := newHeaderRPCServer(t, common.HexToHash("0x01"))
	defer b.Close()
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		if request.Method == "eth_chainId" {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": "0xaa36a7"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "unavailable"}})
	}))
	defer unavailable.Close()
	p, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second, Endpoints: []chainconfig.Endpoint{{ProviderID: "alchemy-a", TrustGroup: "alchemy", URL: a.URL}, {ProviderID: "alchemy-b", TrustGroup: "alchemy", URL: b.URL}, {ProviderID: "independent", TrustGroup: "other", URL: unavailable.URL}}})
	require.NoError(t, err)
	defer p.Close()
	_, err = p.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.Error(t, err, "two Alchemy accounts must not replace an unavailable independent observation")
}

func TestAutoRoutedGatewayCanReadButCannotAttestToCanonicalHeader(t *testing.T) {
	gateway := newHeaderRPCServer(t, common.HexToHash("0xaa"))
	defer gateway.Close()
	p, err := NewRPCPool(t.Context(), RPCPoolConfig{
		NetworkCode: "test", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 2, OpenDuration: time.Second,
		Endpoints: []chainconfig.Endpoint{{ProviderID: "gateway", TrustGroup: "uniblock", URL: gateway.URL}},
	})
	require.NoError(t, err)
	defer p.Close()
	header, err := p.HeaderByNumber(t.Context(), big.NewInt(100))
	require.NoError(t, err)
	require.Equal(t, common.HexToHash("0xaa"), header.ParentHash)
	header, err = p.CanonicalHeaderByNumber(t.Context(), big.NewInt(100))
	require.Error(t, err)
	require.Nil(t, header)
}

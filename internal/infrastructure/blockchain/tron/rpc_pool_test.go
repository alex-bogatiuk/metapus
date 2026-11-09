package tron

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/blockchain/chainconfig"
)

func TestRPCPoolReadFailsOverOnTransientAndStopsOnDeterministicError(t *testing.T) {
	t.Run("transient", func(t *testing.T) {
		var secondaryCalls atomic.Int32
		primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
		}))
		defer primary.Close()
		secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			secondaryCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"balance": 42})
		}))
		defer secondary.Close()

		pool := newTestRPCPool(t, []string{primary.URL}, []string{primary.URL, secondary.URL})
		balance, err := pool.GetTRXBalance(context.Background(), "TAddress")
		require.NoError(t, err)
		require.Equal(t, "42", balance.String())
		require.EqualValues(t, 1, secondaryCalls.Load())
		var primaryState BreakerState
		for _, status := range pool.Status() {
			if status.ProviderID == "solidity-1" {
				primaryState = status.State
			}
		}
		require.Equal(t, BreakerDegraded, primaryState)
	})

	t.Run("deterministic", func(t *testing.T) {
		var secondaryCalls atomic.Int32
		primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		}))
		defer primary.Close()
		secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			secondaryCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"balance": 42})
		}))
		defer secondary.Close()

		pool := newTestRPCPool(t, []string{primary.URL}, []string{primary.URL, secondary.URL})
		_, err := pool.GetTRXBalance(context.Background(), "TAddress")
		require.ErrorContains(t, err, "validation error")
		require.Zero(t, secondaryCalls.Load(), "deterministic 4xx must not fail over")
	})
}

func TestRPCPoolAmbiguousBroadcastReusesExactEnvelope(t *testing.T) {
	var primaryBody, secondaryBody []byte
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case _broadcastPath:
			primaryBody, _ = io.ReadAll(r.Body)
			http.Error(w, "response lost", http.StatusServiceUnavailable)
		case _txInfoPath:
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case _broadcastPath:
			secondaryBody, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(`{"result":true,"txid":"tx-1"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer secondary.Close()

	pool := newTestRPCPool(t, []string{primary.URL, secondary.URL}, []string{secondary.URL})
	outcome, err := pool.BroadcastSignedEnvelope(context.Background(), signedEnvelope(t, "tx-1"))
	require.NoError(t, err)
	require.Equal(t, crypto.BroadcastAccepted, outcome.Status)
	require.JSONEq(t, string(primaryBody), string(secondaryBody))
	require.NotEmpty(t, primaryBody)
}

func TestRPCPoolAmbiguousBroadcastLooksUpBeforeResend(t *testing.T) {
	var secondaryBroadcasts atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == _broadcastPath {
			http.Error(w, "response lost", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == _broadcastPath {
			secondaryBroadcasts.Add(1)
		}
		if r.URL.Path == _txInfoPath {
			_, _ = w.Write([]byte(`{"blockNumber":123}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer secondary.Close()

	pool := newTestRPCPool(t, []string{primary.URL, secondary.URL}, []string{secondary.URL})
	outcome, err := pool.BroadcastSignedEnvelope(context.Background(), signedEnvelope(t, "tx-2"))
	require.NoError(t, err)
	require.Equal(t, crypto.BroadcastAlreadyKnown, outcome.Status)
	require.Zero(t, secondaryBroadcasts.Load(), "visible transaction must not be rebroadcast")
}

func TestRPCPoolProbeMarksLaggingSolidityEndpoint(t *testing.T) {
	roleServer := func(height int64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			block := &BlockInfo{BlockID: strings.Repeat("a", 64)}
			block.BlockHeader.RawData.Number = height
			block.BlockHeader.RawData.Timestamp = time.Now().Add(time.Duration(height-100) * 3 * time.Second).UnixMilli()
			_ = json.NewEncoder(w).Encode(block)
		}))
	}
	primary := roleServer(100)
	defer primary.Close()
	lagging := roleServer(70)
	defer lagging.Close()

	pool := newTestRPCPool(t, []string{primary.URL}, []string{primary.URL, lagging.URL})
	pool.Probe(context.Background())
	statuses := pool.Status()
	var laggingFound bool
	for _, status := range statuses {
		if status.Role == chainconfig.RoleSolidity && status.ProviderID == "solidity-2" {
			laggingFound = true
			require.Equal(t, BreakerLagging, status.State)
		}
	}
	require.True(t, laggingFound)
}

func FuzzNormalizeTRONAddress(f *testing.F) {
	f.Add("TJBwE3H6y2H3c4x1j3eC7T3j4b1c2h3e4b")
	f.Add("  invalid  ")
	f.Add("")
	f.Fuzz(func(t *testing.T, raw string) {
		normalized := NormalizeTronAddress(raw)
		if ValidateTronAddress(normalized) != nil {
			return
		}
		require.Equal(t, normalized, NormalizeTronAddress(normalized))
		require.NoError(t, ValidateTronAddress(normalized))
	})
}

func FuzzDecodeTRONSignedEnvelope(f *testing.F) {
	valid := signedEnvelopeForFuzz("tx-fuzz")
	f.Add([]byte(valid.Payload), valid.TxHash, valid.UnsignedFingerprint, int(valid.SchemaVersion), string(valid.Family))
	f.Add([]byte(`{}`), "tx", "fingerprint", 1, "tron")
	f.Add([]byte(`not-json`), "", "", 0, "evm")
	f.Fuzz(func(t *testing.T, payload []byte, txHash, fingerprint string, schemaVersion int, family string) {
		envelope := crypto.SignedTxEnvelope{
			Family: crypto.ChainFamily(family), SchemaVersion: schemaVersion,
			UnsignedFingerprint: fingerprint, TxHash: txHash, Payload: payload,
		}
		signed, err := decodeTRONSignedEnvelope(envelope)
		if err != nil {
			return
		}
		require.Equal(t, crypto.ChainFamilyTRON, envelope.Family)
		require.Equal(t, tronEnvelopeSchemaVersion, envelope.SchemaVersion)
		require.Equal(t, envelope.TxHash, signed.TxID)
	})
}

func newTestRPCPool(t *testing.T, fullNodes, solidityNodes []string) *RPCPool {
	t.Helper()
	endpoint := func(prefix string, urls []string) []chainconfig.Endpoint {
		result := make([]chainconfig.Endpoint, 0, len(urls))
		for index, url := range urls {
			result = append(result, chainconfig.Endpoint{ProviderID: prefix + "-" + strconv.Itoa(index+1), TrustGroup: "source-" + strconv.Itoa(index+1), Priority: index, URL: url})
		}
		return result
	}
	pool, err := NewRPCPool(RPCPoolConfig{
		NetworkCode: "TRON-NILE", FailureThreshold: 2, OpenDuration: time.Minute,
		FullNode: endpoint("fullnode", fullNodes), Solidity: endpoint("solidity", solidityNodes),
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func signedEnvelope(t *testing.T, txHash string) crypto.SignedTxEnvelope {
	t.Helper()
	return signedEnvelopeForFuzz(txHash)
}

func signedEnvelopeForFuzz(txHash string) crypto.SignedTxEnvelope {
	payload, err := json.Marshal(SignedTransaction{
		TxID: txHash, RawDataHex: "00ff", RawData: json.RawMessage(`{"contract":[]}`),
		Signature: []string{strings.Repeat("a", 130)}, Visible: true,
	})
	if err != nil {
		panic(err)
	}
	return crypto.SignedTxEnvelope{
		Family: crypto.ChainFamilyTRON, SchemaVersion: 1,
		UnsignedFingerprint: strings.Repeat("f", 64), TxHash: txHash, Payload: payload,
	}
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}

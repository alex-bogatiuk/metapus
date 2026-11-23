package evm

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
)

type ingestionDelayTransport struct {
	base    http.RoundTripper
	delay   time.Duration
	headers bool
}

func (d ingestionDelayTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	op, _ := request.Context().Value(rpcOperationContextKey{}).(rpcOperation)
	if op == opBlock || (op == opHeader && d.headers) {
		timer := time.NewTimer(d.delay)
		defer timer.Stop()
		select {
		case <-request.Context().Done():
			return nil, request.Context().Err()
		case <-timer.C:
		}
	}
	return d.base.RoundTrip(request)
}

func TestIngestionCoverageCanUseFullConfiguredFailoverBudget(t *testing.T) {
	a := blockTestServer(t, func(_ http.ResponseWriter, request *http.Request) { <-request.Context().Done() })
	b := blockTestServer(t, nil)
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "TEST", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 3, OpenDuration: time.Minute,
		Settings: chainconfig.EVMSettings{AttemptTimeoutMS: 200, ReadBudgetMS: 500}, Endpoints: []chainconfig.Endpoint{
			{ProviderID: "hanging", URL: a.URL},
			{ProviderID: "reserve", URL: b.URL, WrapTransport: func(base http.RoundTripper) http.RoundTripper {
				return ingestionDelayTransport{base: base, delay: 100 * time.Millisecond}
			}},
		}})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	driver := &Driver{network: crypto.NetworkRef{NetworkIdentifier: "11155111"}, rpc: pool}
	head, err := driver.ReadIngestionHead(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, int64(100), head.Height)
}

func TestIngestionHeaderAndBodyShareOneReadBudget(t *testing.T) {
	server := blockTestServer(t, nil)
	t.Cleanup(server.Close)
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "TEST", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 3, OpenDuration: time.Minute,
		Settings: chainconfig.EVMSettings{AttemptTimeoutMS: 200, ReadBudgetMS: 250}, Endpoints: []chainconfig.Endpoint{
			{ProviderID: "slow", URL: server.URL, WrapTransport: func(base http.RoundTripper) http.RoundTripper {
				return ingestionDelayTransport{base: base, delay: 150 * time.Millisecond, headers: true}
			}},
		}})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	driver := &Driver{network: crypto.NetworkRef{NetworkIdentifier: "11155111"}, rpc: pool}
	_, err = driver.ReadIngestionHead(t.Context(), nil)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "a fresh budget for each stage would incorrectly allow both 150 ms reads")
}

func TestLowQuotaIdentityProbeDoesNotInventProviderFailureOrHeadRecovery(t *testing.T) {
	server := blockTestServer(t, nil)
	t.Cleanup(server.Close)
	pool, err := NewRPCPool(t.Context(), RPCPoolConfig{NetworkCode: "TEST", ExpectedChainID: big.NewInt(11155111), FailureThreshold: 3, OpenDuration: time.Minute,
		Settings: chainconfig.EVMSettings{AttemptTimeoutMS: 10000, ReadBudgetMS: 25000}, Endpoints: []chainconfig.Endpoint{{ProviderID: "reserve", URL: server.URL, MinRequestIntervalMS: 30000}}})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	endpoint := pool.endpoints[0]
	endpoint.mu.Lock()
	require.True(t, endpoint.verified)
	require.Zero(t, endpoint.failures)
	require.Nil(t, endpoint.operations[opHeader], "identity-only probe must not claim a successful head read")
	endpoint.mu.Unlock()
	pool.failure(endpoint, ErrRPCAdmissionBusy)
	require.Zero(t, pool.Status()[0].Failures, "local admission cannot open the legacy endpoint breaker")
}

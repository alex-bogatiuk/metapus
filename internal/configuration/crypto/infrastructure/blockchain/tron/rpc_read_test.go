package tron

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/chainconfig"
	"metapus/pkg/sdk/integrations/providerpolicy"
)

func policyPool(t *testing.T, urls ...string) *RPCPool {
	t.Helper()
	var endpoints []chainconfig.Endpoint
	for i, url := range urls {
		endpoints = append(endpoints, chainconfig.Endpoint{ProviderID: jsonNumber(int64(i)), URL: url})
	}
	p, err := NewRPCPool(RPCPoolConfig{NetworkCode: "TRON-NILE", FullNode: endpoints, Solidity: endpoints, FailureThreshold: 1, OpenDuration: time.Minute, Settings: chainconfig.TRONSettings{AttemptTimeoutMS: 100, ReadBudgetMS: 250, RequestsPerSecond: 1000}})
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}

func TestOneHalfOpenReadAndCallerCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	p := policyPool(t, server.URL)
	e := p.byRole[chainconfig.RoleSolidity][0]
	_, err := p.GetTRXBalance(t.Context(), "account")
	require.Error(t, err)
	e.mu.Lock()
	e.operations["GetTRXBalance"].OpenUntil = time.Now().Add(-time.Second)
	e.mu.Unlock()
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if p.beginRead(e, "GetTRXBalance") {
				admitted.Add(1)
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, admitted.Load())
	p.finishRead(e, "GetTRXBalance", time.Second, context.Canceled, context.Canceled)
	e.mu.Lock()
	snapshot := *e.operations["GetTRXBalance"]
	e.mu.Unlock()
	require.Equal(t, 1, snapshot.Failures)
	require.False(t, snapshot.Probing)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = p.GetTRXBalance(cancelled, "account")
	require.ErrorIs(t, err, context.Canceled)
}

func TestRateLimitAndResponseCapAreTypedAndSanitized(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		want   error
	}{{"429", 429, "credential=secret", providerpolicy.ErrRateLimited}, {"chunked", 200, strings.Repeat("x", 1100), providerpolicy.ErrResponseTooLarge}} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(test.status)
				w.(http.Flusher).Flush()
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			p := policyPool(t, server.URL)
			for _, es := range p.byRole {
				for _, e := range es {
					e.client.maxResponseBytes = 1024
				}
			}
			_, err := p.GetTRXBalance(t.Context(), "account")
			require.ErrorIs(t, err, test.want)
			require.NotContains(t, err.Error(), "secret")
			require.NotContains(t, err.Error(), server.URL)
		})
	}
}

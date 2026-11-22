package cryptoprofiles

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"metapus/pkg/sdk/integrations"
	"metapus/pkg/sdk/integrations/profiles"
	"metapus/pkg/sdk/integrations/providerpolicy"
)

func TestAnkrFreemiumResponses(t *testing.T) {
	profile, found := Registry().Lookup(cryptointegrations.Ankr)
	require.True(t, found)
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		failure integrations.Failure
		opError error
	}{
		{"success", 200, `{"result":"0xaa36a7"}`, "", nil},
		{"rate limit", 429, "", integrations.Throttled, nil},
		{"RPC rate limit", 200, `{"error":{"code":-32090,"message":"call rate limit exhausted"}}`, integrations.Throttled, nil},
		{"code is sufficient for Ankr", 200, `{"error":{"code":-32090,"message":{"reason":"busy"}}}`, integrations.Throttled, nil},
		{"truncated response", 200, `{"error":{"code":-32090},"id":`, "", nil},
		{"invalid key", 401, "", integrations.Unauthorized, nil},
		{"disabled key", 200, `{"error":{"code":-32051}}`, integrations.Unauthorized, nil},
		{"expired key before RPC handling", 403, "message: API key expired, trace_id: test", integrations.Unauthorized, nil},
		{"monthly quota", 402, `{"error":{"message":"monthly quota exceeded"}}`, integrations.Exhausted, nil},
		{"method outside plan", 403, `{"error":{"code":-32053}}`, "", providerpolicy.ErrUnsupported},
		{"method not found", 404, `{"error":{"code":-32601}}`, "", providerpolicy.ErrUnsupported},
		{"endpoint not found", 404, "not found", "", nil},
		{"invalid params", 400, `{"error":{"code":-32602}}`, "", providerpolicy.ErrInvalidRequest},
		{"node response for EVM log handling", 200, `{"error":{"code":-32602,"message":"block range extends beyond current head block"}}`, "", nil},
		{"access denied", 403, "", "", profiles.ErrAccessDenied},
		{"block range", 413, "", "", providerpolicy.ErrQueryLimit},
		{"RPC block range", 200, `{"error":{"code":-32062}}`, "", providerpolicy.ErrQueryLimit},
		{"contract revert", 200, `{"error":{"code":3,"message":"execution reverted: monthly quota exceeded"}}`, "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.failure, profile.Failure(tt.status, []byte(tt.body)))
			if tt.failure == "" {
				require.ErrorIs(t, profile.ResponseError(tt.status, []byte(tt.body)), tt.opError)
			}
		})
	}
}

func TestAnkrAuthorizesOnlySepoliaWithASeparateKey(t *testing.T) {
	profile, found := Registry().Lookup(cryptointegrations.Ankr)
	require.True(t, found)
	const base = "https://rpc.ankr.com/eth_sepolia"
	key := strings.Repeat("ab", 32)
	req, err := http.NewRequest(http.MethodPost, base+"/", nil)
	require.NoError(t, err)
	require.NoError(t, profile.Authorize(req, key))
	require.Equal(t, base+"/"+key, req.URL.String())
	for _, tt := range []struct{ url, key string }{
		{base + "/" + key, key}, {base + "?key=extra", key}, {base, "key/extra"},
		{"https://rpc.ankr.com/eth", key}, {"https://rpc.ankr.com.evil.test/eth_sepolia", key},
		{"https://rpc.ankr.com/%65th_sepolia", key}, {"http://rpc.ankr.com/eth_sepolia", key},
	} {
		req, err := http.NewRequest(http.MethodPost, tt.url, nil)
		require.NoError(t, err)
		err = profile.Authorize(req, tt.key)
		require.Error(t, err)
		require.NotContains(t, err.Error(), tt.key)
		require.Equal(t, tt.url, req.URL.String())
	}
}

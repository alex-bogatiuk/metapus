package cryptoprofiles

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/crypto/cryptointegrations"
	"metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/integrations/profiles"
	"metapus/internal/infrastructure/providerpolicy"
)

func TestProjectRPCBindingsRefuseUnsafeRequestsBeforeAuthorization(t *testing.T) {
	for _, tt := range []struct {
		provider integrations.Provider
		base     string
		extra    []string
	}{
		{cryptointegrations.ValidationCloud, "https://sepolia.ethereum.validationcloud.io/v1", []string{"https://ethereum.validationcloud.io/v1"}},
		{cryptointegrations.ZAN, "https://api.zan.top/node/v1/eth/sepolia", []string{
			"https://api.zan.top/node/v1/eth/sepolia/wallet/getnowblock",
			"https://api.zan.top/node/v1/tron/nile", "https://api.zan.top/node/v1/tron/nile/jsonrpc",
			"https://api.zan.top/node/v1/tron/nile//wallet/getnowblock",
			"https://api.zan.top/node/v1/tron/nile/wallet/getnowblock/extra",
			"https://api.zan.top/node/v1/tron/nile/wallet/%67etnowblock",
			"https://api.zan.top/node/v1/tron/mainnet/wallet/getnowblock",
		}},
	} {
		profile, ok := Registry().Lookup(tt.provider)
		require.True(t, ok)
		for _, raw := range append(tt.extra, tt.base+"/test-token", tt.base+"?", tt.base+"#ref", "https://foreign.test/") {
			req, err := http.NewRequest(http.MethodPost, raw, nil)
			require.NoError(t, err)
			err = profile.Authorize(req, "test-token")
			require.Error(t, err, raw)
			require.NotContains(t, err.Error(), "test-token")
			require.Empty(t, req.Header)
			require.Equal(t, raw, req.URL.String())
		}
		req, err := http.NewRequest(http.MethodPost, tt.base, nil)
		require.NoError(t, err)
		require.Error(t, profile.Authorize(req, "bad/key"))
	}
}

func TestProjectRPCGatewayErrors(t *testing.T) {
	for _, tt := range []struct {
		provider integrations.Provider
		status   int
		body     string
		failure  integrations.Failure
		opError  error
	}{
		{cryptointegrations.ValidationCloud, 200, `{"error":{"code":3,"message":"execution reverted: monthly quota exceeded"}}`, "", nil},
		{cryptointegrations.ValidationCloud, 401, "", integrations.Unauthorized, nil},
		{cryptointegrations.ValidationCloud, 429, "", integrations.Throttled, nil},
		{cryptointegrations.ZAN, 403, `{"error":{"code":-32005}}`, integrations.Exhausted, nil},
		{cryptointegrations.ZAN, 403, `{"error_code":-32004}`, integrations.Throttled, nil},
		{cryptointegrations.ZAN, 403, `{"error_code":-32002}`, "", profiles.ErrAccessDenied},
		{cryptointegrations.ZAN, 403, `{"error":{"code":-32007}}`, "", providerpolicy.ErrUnsupported},
		{cryptointegrations.ZAN, 413, "", "", providerpolicy.ErrQueryLimit},
		{cryptointegrations.ZAN, 200, `{"error":{"code":-32000,"message":"node error"}}`, "", nil},
	} {
		profile, ok := Registry().Lookup(tt.provider)
		require.True(t, ok)
		require.Equal(t, tt.failure, profile.Failure(tt.status, []byte(tt.body)), tt.body)
		if tt.failure == "" && profile.ResponseError != nil {
			require.ErrorIs(t, profile.ResponseError(tt.status, []byte(tt.body)), tt.opError)
		}
	}
}

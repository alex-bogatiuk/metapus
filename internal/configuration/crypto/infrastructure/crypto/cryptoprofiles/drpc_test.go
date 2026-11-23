package cryptoprofiles

import (
	"fmt"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/cryptointegrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/profiles"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/integrations/providerpolicy"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestDRPCUsesSuffixAndSeparatesQueryLimitsFromAccountFailures(t *testing.T) {
	p := drpc()
	req, _ := http.NewRequest(http.MethodPost, "https://lb.drpc.live/sepolia/", nil)
	req.Header.Set("Authorization", "foreign")
	require.NoError(t, p.Authorize(req, "opaque-key"))
	require.Equal(t, "/sepolia/opaque-key", req.URL.Path)
	require.Empty(t, req.Header.Get("Authorization"))
	body := []byte(`{"error":{"code":35,"message":"Ranges over 10000 BLOCKS are not supported on free plan"}}`)
	require.Empty(t, p.Failure(400, body))
	require.ErrorIs(t, p.ResponseError(400, body), providerpolicy.ErrQueryLimit)
	require.ErrorIs(t, p.ResponseError(403, body), profiles.ErrAccessDenied)
	require.Empty(t, p.Failure(200, []byte(`{"error":{"code":10,"message":{}}}`)), "malformed dRPC error must not suspend the account")
	require.Nil(t, p.ResponseError(400, []byte(`{"error":{"code":35,"message":"other failure"}}`)))
	chainDenied := []byte(`{"error":{"code":35,"message":"chain is not available on free plan, please upgrade to paid plan"}}`)
	require.Empty(t, p.Failure(400, chainDenied))
	require.ErrorIs(t, p.ResponseError(400, chainDenied), providerpolicy.ErrAccessDenied)
	require.NotErrorIs(t, p.ResponseError(400, chainDenied), providerpolicy.ErrQueryLimit)
	require.Equal(t, integrations.Throttled, p.Failure(429, []byte(`{"error":"reduce request rate"}`)))
	require.Equal(t, integrations.Unauthorized, p.Failure(401, []byte(`{}`)))
	for _, code := range []int{10, 52, 53} {
		require.Equal(t, integrations.Exhausted, p.Failure(403, []byte(fmt.Sprintf(`{"error":{"code":%d,"message":"billing limit"}}`, code))))
	}
	for _, code := range []int{4, 9, 32} {
		require.Equal(t, integrations.Unauthorized, p.Failure(403, []byte(fmt.Sprintf(`{"error":{"code":%d}}`, code))))
	}
	for _, code := range []int{29, 47, 48, 50, 51, 99} {
		require.Empty(t, p.Failure(403, []byte(fmt.Sprintf(`{"error":{"code":%d}}`, code))))
	}
	require.ErrorIs(t, p.ResponseError(403, []byte(`{"error":{"code":48}}`)), providerpolicy.ErrUnsupported)
	require.ErrorIs(t, p.ResponseError(403, []byte(`{"error":{"code":99}}`)), profiles.ErrAccessDenied)
	now := time.Date(2026, 10, 29, 12, 30, 0, 0, time.UTC)
	daily := []byte(`{"error":{"code":24}}`)
	require.Equal(t, integrations.Throttled, p.Failure(403, daily))
	require.Equal(t, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), p.NextAttempt(403, http.Header{}, daily, now))
	bad, _ := http.NewRequest(http.MethodPost, "https://lb.drpc.live/sepolia/secret", nil)
	require.Error(t, p.Authorize(bad, "key"))
	provider, base, key, err := ImportRPC("https://lb.drpc.live/sepolia/", "private-key", "rpc", "sepolia")
	require.NoError(t, err)
	require.Equal(t, cryptointegrations.DRPC, provider)
	require.Equal(t, "https://lb.drpc.live/sepolia/", base)
	require.Equal(t, "private-key", key)
}

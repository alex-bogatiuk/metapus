package integrations

import (
	"net/http"
	"strings"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
	model "github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/integrations/profiles"
	"github.com/stretchr/testify/require"
)

// fixtureProbe stands in for the configuration's endpoint probe: it posts to
// the binding URL, or to a TRON role path, and accepts any 200 body.
func fixtureProbe(target model.ConnectionTarget) (profiles.Probe, error) {
	probe := profiles.Probe{Method: http.MethodPost, URL: target.URL, Body: `{}`, Valid: func([]byte) bool { return true }}
	switch target.Attributes["role"] {
	case "fullnode":
		probe.URL = strings.TrimRight(target.URL, "/") + "/wallet/getnowblock"
	case "solidity":
		probe.URL = strings.TrimRight(target.URL, "/") + "/walletsolidity/getnowblock"
	}
	return probe, nil
}

func TestConnectionProberUsesActualBindingAndRejectsRedirects(t *testing.T) {
	for _, status := range []int{200, 302} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			target := model.ConnectionTarget{PoolID: id.New(), EndpointID: id.New(), Provider: "fixture_suffix", URL: "https://fixture.test/v2/"}
			calls := 0
			prober := ConnectionProber{Profiles: fixtureProfiles, EndpointProbe: fixtureProbe, EndpointTransport: func(uid id.ID, base string) func(http.RoundTripper) http.RoundTripper {
				require.Equal(t, target.EndpointID, uid)
				require.Equal(t, target.URL, base)
				return func(http.RoundTripper) http.RoundTripper {
					return roundTripFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						require.Equal(t, target.URL, req.URL.String())
						resp := response(req, status, `{"result":"0x1"}`)
						resp.Header.Set("Location", "https://foreign.test/private-token")
						return resp, nil
					})
				}
			}}
			ready, err := prober.Probe(t.Context(), target)
			require.NoError(t, err)
			require.Equal(t, status == 200, ready)
			require.Equal(t, 1, calls)
		})
	}
}

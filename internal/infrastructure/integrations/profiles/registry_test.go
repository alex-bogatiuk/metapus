package profiles

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistryRejectsDuplicatesAndDoesNotExposeMutableProbeDefinitions(t *testing.T) {
	authorize := func(*http.Request, string) error { return nil }
	_, err := New(Profile{Provider: "new", Authorize: authorize}, Profile{Provider: "new", Authorize: authorize})
	require.Error(t, err)
	_, err = New(Profile{Provider: "new"})
	require.Error(t, err)
	p := Profile{Provider: "new", Authorize: authorize, Probe: &Probe{URL: "https://provider.test/ping"}}
	r, err := New(p)
	require.NoError(t, err)
	p.Probe.URL = "https://foreign.test"
	got, ok := r.Lookup("new")
	require.True(t, ok)
	require.Equal(t, "https://provider.test/ping", got.Probe.URL)
	got.Probe.URL = "https://foreign.test"
	again, _ := r.Lookup("new")
	require.Equal(t, "https://provider.test/ping", again.Probe.URL)
	_, ok = r.Lookup("missing")
	require.False(t, ok)
}

// Package sdktest holds the checks a configuration runs in its own tests.
package sdktest

import (
	"testing"

	"metapus/internal/configuration"
	"metapus/internal/configuration/configurationtest"
)

// Check verifies that cfg keeps the configuration contract: its API
// version, repeatable registration, unique permissions and routes, contract
// generation, the platform's integration providers and its migrations.
func Check(t *testing.T, cfg configuration.Configuration) {
	t.Helper()
	configurationtest.Check(t, cfg)
}

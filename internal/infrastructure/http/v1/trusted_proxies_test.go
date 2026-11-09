package v1

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConfigureTrustedProxiesRejectsInvalidStartupConfiguration(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "not-a-network")
	require.Panics(t, func() { configureTrustedProxies(gin.New(), nil) })
}

func TestConfigureTrustedProxiesAcceptsExplicitCIDRs(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1, 10.0.0.0/8")
	require.NotPanics(t, func() { configureTrustedProxies(gin.New(), nil) })
}

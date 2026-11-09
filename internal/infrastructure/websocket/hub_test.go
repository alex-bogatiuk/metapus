package websocket

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckOriginProductionUsesExactAllowlist(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_ALLOWED_ORIGINS", "https://176.215.182.82, https://admin.example.test")

	request := httptest.NewRequest("GET", "https://176.215.182.82/api/v1/ws", nil)
	request.Header.Set("Origin", "https://176.215.182.82")
	require.True(t, checkOrigin(request))

	request.Header.Set("Origin", "https://176.215.182.82.attacker.example")
	require.False(t, checkOrigin(request))
}

func TestCheckOriginProductionFailsClosedWithoutAllowlist(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_ALLOWED_ORIGINS", "")

	request := httptest.NewRequest("GET", "https://176.215.182.82/api/v1/ws", nil)
	request.Header.Set("Origin", "https://176.215.182.82")
	require.False(t, checkOrigin(request))
}

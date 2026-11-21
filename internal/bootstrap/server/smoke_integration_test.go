//go:build integration

package server

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"metapus/internal/configuration/none"
	"metapus/pkg/logger"
)

// The platform's server starts with the empty configuration and none of an
// industry configuration's settings (no Vault, no checkout URL), serves its
// health probes and stops cleanly.
func TestServerStartsWithTheEmptyConfiguration(t *testing.T) {
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("METAPUS_TEST_DATABASE_DSN is required")
	}
	conn, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	for key, value := range map[string]string{
		"APP_ENV":                  "development",
		"APP_PORT":                 strconv.Itoa(port),
		"META_DATABASE_URL":        dsn,
		"TENANT_DB_USER":           conn.User,
		"TENANT_DB_PASSWORD":       conn.Password,
		"JWT_SECRET":               "smoke-jwt-secret",
		"AUTH_MFA_ENCRYPTION_KEY":  base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"VAULT_ADDR":               "",
		"PUBLIC_CHECKOUT_BASE_URL": "",
	} {
		t.Setenv(key, value)
	}
	settings, err := loadConfig()
	require.NoError(t, err)
	log, err := logger.New(logger.Config{Level: "error"})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, settings, Build{Version: "smoke"}, none.New(), nil, log) }()

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	require.Eventually(t, func() bool {
		response, err := http.Get(base + "/health/ready")
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode == http.StatusOK
	}, 15*time.Second, 50*time.Millisecond, "the server never became ready")

	response, err := http.Get(base + "/merchant/v1/invoices")
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, http.StatusNotFound, response.StatusCode, "the empty configuration mounts no configuration routes")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(35 * time.Second):
		t.Fatal("the server did not stop")
	}
}

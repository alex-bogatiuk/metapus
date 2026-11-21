package server

import (
	"encoding/base64"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/domain/auth"
)

// setValidServerEnv sets the minimum environment loadConfig accepts: no
// Vault and no configuration settings.
func setValidServerEnv(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"APP_ENV":                 "development",
		"META_DATABASE_URL":       "postgres://meta",
		"TENANT_DB_USER":          "tenant",
		"TENANT_DB_PASSWORD":      "tenant-password",
		"JWT_SECRET":              "jwt-secret",
		"AUTH_MFA_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)),
	} {
		t.Setenv(key, value)
	}
}

func TestLoadConfigAppliesDefaults(t *testing.T) {
	setValidServerEnv(t)

	cfg, err := loadConfig()

	require.NoError(t, err)
	require.Equal(t, "8080", cfg.Port)
	require.Equal(t, 100, cfg.TenantMaxPools)
	require.Equal(t, 10, cfg.TenantMaxConnsPerPool)
	require.Equal(t, 30*time.Minute, cfg.TenantPoolIdleTimeout)
	require.Equal(t, 5*time.Minute, cfg.AuthStateCacheTTL)
	require.Equal(t, 168*time.Hour, cfg.Auth.AdminMFAGracePeriod)
	require.Equal(t, 300*time.Second, cfg.Auth.AdminStepUpTTL)
	require.Len(t, cfg.Auth.MFAEncryptionKey, 32)
}

func TestLoadConfigParsesOverrides(t *testing.T) {
	setValidServerEnv(t)
	t.Setenv("TENANT_POOL_IDLE_TIMEOUT", "90s")
	t.Setenv("ADMIN_MFA_GRACE_PERIOD_HOURS", "0")
	t.Setenv("ADMIN_MFA_MODE", " Required ")
	t.Setenv("ADMIN_SECURITY_MODE", "enforce")
	t.Setenv("ADMIN_ALLOWED_CIDRS", "10.0.0.0/8, 192.168.1.7,")

	cfg, err := loadConfig()

	require.NoError(t, err)
	require.Equal(t, 90*time.Second, cfg.TenantPoolIdleTimeout)
	require.Zero(t, cfg.Auth.AdminMFAGracePeriod)
	require.Equal(t, auth.MFAModeRequired, cfg.Auth.AdminMFAMode)
	require.Equal(t, auth.AdminSecurityEnforce, cfg.Auth.AdminSecurityMode)
	require.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("192.168.1.7/32"),
	}, cfg.Auth.AdminAllowedCIDRs)
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		env   map[string]string
		error string
	}{
		{name: "missing required value", env: map[string]string{"JWT_SECRET": ""}, error: "JWT_SECRET"},
		{name: "integer with garbage", env: map[string]string{"TENANT_MAX_POOLS": "10abc"}, error: "TENANT_MAX_POOLS"},
		{name: "zero pool size", env: map[string]string{"TENANT_MAX_CONNS_PER_POOL": "0"}, error: "TENANT_MAX_CONNS_PER_POOL"},
		{name: "duration in step-up seconds", env: map[string]string{"ADMIN_STEP_UP_TTL_SECONDS": "5m"}, error: "ADMIN_STEP_UP_TTL_SECONDS"},
		{name: "bare number cache ttl", env: map[string]string{"AUTH_STATE_CACHE_TTL": "300"}, error: "AUTH_STATE_CACHE_TTL"},
		{name: "unknown mfa mode", env: map[string]string{"ADMIN_MFA_MODE": "always"}, error: "ADMIN_MFA_MODE"},
		{name: "unknown security mode", env: map[string]string{"ADMIN_SECURITY_MODE": "strict"}, error: "ADMIN_SECURITY_MODE"},
		{name: "invalid cidr", env: map[string]string{"ADMIN_ALLOWED_CIDRS": "10.0.0.0/8,not-an-ip"}, error: "ADMIN_ALLOWED_CIDRS"},
		{name: "short mfa key", env: map[string]string{"AUTH_MFA_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(make([]byte, 16))}, error: "AUTH_MFA_ENCRYPTION_KEY"},
		{name: "missing mfa key", env: map[string]string{"AUTH_MFA_ENCRYPTION_KEY": ""}, error: "AUTH_MFA_ENCRYPTION_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidServerEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, err := loadConfig()

			require.ErrorContains(t, err, tt.error)
		})
	}
}

func TestLoadConfigReportsEveryInvalidValue(t *testing.T) {
	setValidServerEnv(t)
	t.Setenv("JWT_SECRET", "")
	t.Setenv("TENANT_MAX_POOLS", "many")
	t.Setenv("ADMIN_MFA_MODE", "always")

	_, err := loadConfig()

	require.ErrorContains(t, err, "JWT_SECRET")
	require.ErrorContains(t, err, "TENANT_MAX_POOLS")
	require.ErrorContains(t, err, "ADMIN_MFA_MODE")
}

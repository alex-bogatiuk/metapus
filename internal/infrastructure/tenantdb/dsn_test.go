package tenantdb

import (
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/stretchr/testify/require"
	"net/url"
	"testing"
)

func TestTenantDSNEscapesCredentials(t *testing.T) {
	entry := &tenant.Tenant{DBHost: "localhost", DBPort: 5432, DBName: "mt_acme-eu"}
	dsn := DSN(entry, "user@example.com", "p@ss:/word")

	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "user@example.com", parsed.User.Username())
	password, ok := parsed.User.Password()
	require.True(t, ok)
	require.Equal(t, "p@ss:/word", password)
	require.Equal(t, "/mt_acme-eu", parsed.Path)
	require.Equal(t, "disable", parsed.Query().Get("sslmode"))
}

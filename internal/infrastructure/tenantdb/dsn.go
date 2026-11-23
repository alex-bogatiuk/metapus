package tenantdb

import (
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"net"
	"net/url"
	"strconv"
)

// DSN builds PostgreSQL connection string for this tenant's database.
func DSN(t *tenant.Tenant, user, password string) string {
	return DSNWithSSL(t, user, password, "disable")
}

// DSNWithSSL builds PostgreSQL connection string with SSL enabled.
func DSNWithSSL(t *tenant.Tenant, user, password, sslMode string) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(t.DBHost, strconv.Itoa(t.DBPort)),
		Path:   "/" + t.DBName,
	}
	u.RawQuery = url.Values{"sslmode": []string{sslMode}}.Encode()
	return u.String()
}

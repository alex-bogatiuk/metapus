// Command tenant manages tenants of the platform with the empty
// configuration and the client extensions (internal/bootstrap/tenant). A
// configuration has a tenant command of its own (cmd/crypto-tenant).
package main

import (
	"metapus/extensions"
	"metapus/internal/bootstrap/tenant"
	"metapus/internal/configuration/none"
)

func main() { tenant.Main(none.New(), extensions.Register) }

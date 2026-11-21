// Command crypto-tenant manages tenants of the crypto-processing
// configuration (internal/bootstrap/tenant): they get the platform's and the
// crypto migrations.
package main

import (
	"metapus/extensions"
	"metapus/internal/bootstrap/tenant"
	"metapus/internal/configuration/crypto/content"
)

func main() { tenant.Main(content.Crypto(content.Services{}), extensions.Register) }

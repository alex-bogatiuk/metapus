// Command autoparts-tenant manages tenants of the auto parts store
// configuration (internal/bootstrap/tenant): they get the platform's and the
// auto parts migrations.
package main

import (
	"metapus/extensions"
	"metapus/internal/bootstrap/tenant"
	"metapus/internal/configuration/autoparts/content"
)

func main() { tenant.Main(content.Autoparts(), extensions.Register) }

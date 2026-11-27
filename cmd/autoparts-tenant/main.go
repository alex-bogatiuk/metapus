// Command autoparts-tenant manages tenants of the add-on «Автозапчасти»
// on «Торговля и сервис» (internal/bootstrap/tenant): they get the platform's and the
// trade and auto parts migrations.
package main

import (
	"github.com/alex-bogatiuk/metapus/configurations/autoparts/content"
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Tenant(content.AutoParts(), launch.WithExtensions(extensions.Register)) }

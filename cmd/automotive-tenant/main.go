// Command automotive-tenant manages tenants of the add-on «Автобизнес»
// on «Торговля и сервис» (internal/bootstrap/tenant): they get the platform's and the
// trade and automotive migrations.
package main

import (
	"github.com/alex-bogatiuk/metapus/configurations/automotive/content"
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Tenant(content.Automotive(), launch.WithExtensions(extensions.Register)) }

// Command trade-tenant manages tenants of the trade configuration
// («Торговля и сервис») (internal/bootstrap/tenant): they get the platform's and the
// trade migrations.
package main

import (
	"github.com/alex-bogatiuk/metapus/configurations/trade/content"
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Tenant(content.Trade(), launch.WithExtensions(extensions.Register)) }

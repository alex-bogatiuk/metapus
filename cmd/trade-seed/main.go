// Command trade-seed seeds a tenant database of the trade configuration
// («Торговля и сервис») (internal/bootstrap/seed): the platform's data, then the
// trade demo warehouses and items with SEED_DEMO_DATA=true.
package main

import (
	"github.com/alex-bogatiuk/metapus/configurations/trade/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Seed(content.Trade()) }

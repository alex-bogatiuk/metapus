// Command autoparts-seed seeds a tenant database of the add-on «Автозапчасти»
// on «Торговля и сервис» (internal/bootstrap/seed): the platform's data, then the
// trade demo warehouses and items with SEED_DEMO_DATA=true.
package main

import (
	"github.com/alex-bogatiuk/metapus/configurations/autoparts/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Seed(content.AutoParts()) }

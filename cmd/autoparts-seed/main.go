// Command autoparts-seed seeds a tenant database of the auto parts store
// configuration (internal/bootstrap/seed): the platform's data, then the
// auto parts demo warehouses and items with SEED_DEMO_DATA=true.
package main

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Seed(content.Autoparts()) }

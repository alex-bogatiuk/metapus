// Command autoparts-seed seeds a tenant database of the auto parts store
// configuration (internal/bootstrap/seed): the platform's data, then the
// auto parts demo warehouses and items with SEED_DEMO_DATA=true.
package main

import (
	"metapus/internal/bootstrap/seed"
	"metapus/internal/configuration/autoparts/content"
)

func main() { seed.Main(content.Autoparts()) }

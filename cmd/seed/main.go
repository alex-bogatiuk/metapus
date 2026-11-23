// Command seed seeds a tenant database of the platform with the empty
// configuration (internal/bootstrap/seed). A configuration has a seed command
// of its own (cmd/crypto-seed).
package main

import (
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/seed"
	"github.com/alex-bogatiuk/metapus/internal/configuration/none"
)

func main() { seed.Main(none.New()) }

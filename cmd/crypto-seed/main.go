// Command crypto-seed seeds a tenant database of the crypto-processing
// configuration (internal/bootstrap/seed): the platform's data, then the
// crypto demo data with SEED_DEMO_DATA=true.
package main

import (
	"github.com/alex-bogatiuk/metapus/internal/bootstrap/seed"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
)

func main() { seed.Main(content.Crypto(content.Services{})) }

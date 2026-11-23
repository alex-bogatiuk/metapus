// Command crypto-seed seeds a tenant database of the crypto-processing
// configuration (internal/bootstrap/seed): the platform's data, then the
// crypto demo data with SEED_DEMO_DATA=true.
package main

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Seed(content.Crypto(content.Services{})) }

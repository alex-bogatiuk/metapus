// Command crypto-tenant manages tenants of the crypto-processing
// configuration (internal/bootstrap/tenant): they get the platform's and the
// crypto migrations.
package main

import (
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() {
	launch.Tenant(content.Crypto(content.Services{}), launch.WithExtensions(extensions.Register))
}

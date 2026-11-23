// Command autoparts-tenant manages tenants of the auto parts store
// configuration (internal/bootstrap/tenant): they get the platform's and the
// auto parts migrations.
package main

import (
	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/internal/configuration/autoparts/content"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/launch"
)

func main() { launch.Tenant(content.Autoparts(), launch.WithExtensions(extensions.Register)) }

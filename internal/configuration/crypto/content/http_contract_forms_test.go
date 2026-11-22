package content_test

import (
	"metapus/pkg/sdk/configuration"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/content"
	"metapus/pkg/sdk/contracts"
	"metapus/pkg/sdk/http/v1"
)

func TestEveryBuiltInFactoryHasConcreteFormContracts(t *testing.T) {
	factories := configuration.Compose(content.Crypto(content.Services{}))
	routes := map[string]contracts.RouteContract{}
	for _, route := range contracts.Routes(configuration.HTTPContractBundles(content.Crypto(content.Services{}), configuration.Extensions)...) {
		routes[route.Scope+" "+route.Method+" "+route.Path] = route
	}
	check := func(prefix string, canCreate, canUpdate bool) {
		operations := []struct{ method, suffix string }{{"GET", "/:id"}}
		if canCreate {
			operations = append(operations, struct{ method, suffix string }{"POST", ""})
		}
		if canUpdate {
			operations = append(operations, struct{ method, suffix string }{"PUT", "/:id"})
		}
		for _, operation := range operations {
			key := "api " + operation.method + " " + prefix + operation.suffix
			route, ok := routes[key]
			require.True(t, ok, "factory lacks HTTP contract: %s", key)
			require.NotEmpty(t, route.Response, key)
			if operation.method != "GET" {
				require.NotEmpty(t, route.Request, key)
			}
		}
	}
	for _, factory := range factories.Catalogs() {
		check("/catalog/"+factory.RoutePrefix(), true, true)
	}
	for _, factory := range factories.Documents() {
		canCreate, canUpdate := true, true
		if provider, ok := factory.(v1.DocumentCapabilitiesProvider); ok {
			caps := provider.DocumentCapabilities()
			canCreate = caps.Create
			canUpdate = caps.Update
		}
		check("/document/"+factory.RoutePrefix(), canCreate, canUpdate)
	}
}

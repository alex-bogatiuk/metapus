package content_test

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/content"
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptohandlers"
	"metapus/pkg/sdk/configuration"
	"metapus/pkg/sdk/contracts"
)

// The crypto service routes have wire contracts; the platform's own are
// checked by the platform (configuration/none).
func TestServiceRouteRegistrationsHaveWireContracts(t *testing.T) {
	router := gin.New()
	root := router.Group("")
	new(cryptohandlers.WithdrawalRequestWorkflowHandler).RegisterDocumentRoutes(router.Group("/document/withdrawal-request"), "document:withdrawal_request")
	new(cryptohandlers.CryptoProviderBalanceOperationHandler).RegisterCustomDocumentRoutes(router.Group("/document/crypto-provider-balance-operation"), "document:crypto_provider_balance_operation")
	guard := func(c *gin.Context) { c.Next() }
	cryptohandlers.NewWalletPoolHandler(nil, nil).RegisterRoutes(root, guard)
	cryptohandlers.NewCryptoNetworkHandler(nil, nil, nil).RegisterRoutes(root, guard)
	cryptohandlers.NewCryptoRPCConfigurationHandler(nil).RegisterRoutes(root, guard)
	registry := map[string]contracts.RouteContract{}
	for _, route := range contracts.Routes(configuration.HTTPContractBundles(content.Crypto(content.Services{}), configuration.Extensions)...) {
		if route.Scope == "api" {
			registry[route.Method+" "+route.Path] = route
		}
	}
	for _, route := range router.Routes() {
		_, ok := registry[route.Method+" "+route.Path]
		require.True(t, ok, "unregistered service contract: %s %s", route.Method, route.Path)
	}
}

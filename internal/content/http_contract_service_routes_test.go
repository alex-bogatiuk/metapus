package content_test

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"metapus/extensions"
	"metapus/internal/configuration"
	"metapus/internal/content"
	"metapus/internal/infrastructure/http/contracts"
	"metapus/internal/infrastructure/http/cryptohttp/cryptohandlers"
	"metapus/internal/infrastructure/http/v1/handlers"
)

func TestServiceRouteRegistrationsHaveWireContracts(t *testing.T) {
	router := gin.New()
	system := router.Group("/system")
	handlers.NewAutomationAccountHandler(nil, nil, nil).RegisterRoutes(system)
	handlers.NewAutomationChannelHandler(nil, nil).RegisterRoutes(system)
	handlers.NewAutomationRuleHandler(nil, nil).RegisterRoutes(system)
	handlers.NewAutomationHistoryHandler(nil, nil).RegisterRoutes(system)
	handlers.NewAutomationMetaHandler(nil, nil).RegisterRoutes(system)
	root := router.Group("")
	new(handlers.AuthHandler).RegisterRoutes(router.Group("/auth"), router.Group("/auth"), nil)
	new(cryptohandlers.WithdrawalRequestWorkflowHandler).RegisterDocumentRoutes(router.Group("/document/withdrawal-request"), "document:withdrawal_request")
	new(cryptohandlers.CryptoProviderBalanceOperationHandler).RegisterCustomDocumentRoutes(router.Group("/document/crypto-provider-balance-operation"), "document:crypto_provider_balance_operation")
	guard := func(c *gin.Context) { c.Next() }
	cryptohandlers.NewWalletPoolHandler(nil, nil).RegisterRoutes(root, guard)
	cryptohandlers.NewCryptoNetworkHandler(nil, nil, nil).RegisterRoutes(root, guard)
	cryptohandlers.NewCryptoRPCConfigurationHandler(nil).RegisterRoutes(root, guard)
	handlers.RegisterListViewRoutes(root, nil)
	registry := map[string]contracts.RouteContract{}
	for _, route := range contracts.Routes(configuration.HTTPContractBundles(content.Crypto(content.Services{}), extensions.Register)...) {
		if route.Scope == "api" {
			registry[route.Method+" "+route.Path] = route
		}
	}
	for _, route := range router.Routes() {
		_, ok := registry[route.Method+" "+route.Path]
		require.True(t, ok, "unregistered service contract: %s %s", route.Method, route.Path)
	}
}

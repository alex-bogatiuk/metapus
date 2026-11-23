package none_test

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/extensions"
	"github.com/alex-bogatiuk/metapus/internal/configuration"
	"github.com/alex-bogatiuk/metapus/internal/configuration/none"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/contracts"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
)

// The platform's service routes (automation, authentication, list views)
// have wire contracts in every assembled application, the platform alone
// included. A configuration checks its own service routes against the same
// contracts.
func TestPlatformServiceRoutesHaveWireContracts(t *testing.T) {
	router := gin.New()
	system := router.Group("/system")
	handlers.NewAutomationAccountHandler(nil, nil, nil).RegisterRoutes(system)
	handlers.NewAutomationChannelHandler(nil, nil).RegisterRoutes(system)
	handlers.NewAutomationRuleHandler(nil, nil).RegisterRoutes(system)
	handlers.NewAutomationHistoryHandler(nil, nil).RegisterRoutes(system)
	handlers.NewAutomationMetaHandler(nil, nil).RegisterRoutes(system)
	new(handlers.AuthHandler).RegisterRoutes(router.Group("/auth"), router.Group("/auth"), nil)
	handlers.RegisterListViewRoutes(router.Group(""), nil)
	registry := map[string]bool{}
	for _, route := range contracts.Routes(configuration.HTTPContractBundles(none.New(), extensions.Register)...) {
		if route.Scope == "api" {
			registry[route.Method+" "+route.Path] = true
		}
	}
	for _, route := range router.Routes() {
		require.True(t, registry[route.Method+" "+route.Path], "unregistered service contract: %s %s", route.Method, route.Path)
	}
}

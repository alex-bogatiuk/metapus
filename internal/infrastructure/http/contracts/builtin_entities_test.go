package contracts_test

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"metapus/internal/content"
	"metapus/internal/core/entity"
	"metapus/internal/core/numerator"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/posting"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo"
)

// No command is executed. These non-nil ports permit building all production
// handlers so the test compares actual Gin registration with the wire bundle.
type routeStepUp struct{ middleware.StepUpChecker }
type routeSigners struct {
	crypto.MoneyMovementApprovalSignerProvider
}

func TestEveryBuiltInEntityDerivesIdentityAndConcreteRoutesFromItsDefinition(t *testing.T) {
	gin.SetMode(gin.TestMode)
	factories := v1.NewFactoryRegistry()
	content.RegisterDefaults(factories)
	deps := v1.DocumentDeps{BaseHandler: handlers.NewBaseHandler(), Numerator: numerator.Noop(), PostingEngine: posting.NewEngine(nil), MoneyMovementApprovals: money_movement_repo.New(), MoneyMovementSigners: routeSigners{}, StepUpChecker: routeStepUp{}, StepUpGuard: func(string) gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }}
	identities := map[string]bool{}
	checkIdentity := func(reg entity.DefinitionProvider) {
		def := reg.EntityDefinition()
		require.NoError(t, def.Validate())
		require.False(t, identities[def.Key], "duplicate canonical key %s", def.Key)
		identities[def.Key] = true
	}
	checkRoutes := func(t *testing.T, router *gin.Engine, declared map[string]bool) {
		for _, route := range router.Routes() {
			key := route.Method + " " + route.Path
			if !declared[key] {
				t.Errorf("handler lacks a declared contract: %s", key)
			}
			delete(declared, key)
		}
	}
	for _, reg := range factories.Catalogs() {
		t.Run(reg.EntityName(), func(t *testing.T) {
			checkIdentity(reg)
			model := reg.(interface{ EntityStruct() any }).EntityStruct().(entity.DefinitionProvider)
			require.Equal(t, model.EntityDefinition(), reg.EntityDefinition())
			router := gin.New()
			v1.RegisterCatalogRoutes(router.Group("/catalog/"+reg.RoutePrefix()), reg.Build(v1.CatalogDeps{BaseHandler: deps.BaseHandler, Numerator: deps.Numerator}), reg.Permission())
			declared := map[string]bool{}
			for _, route := range reg.HTTPContracts().Routes {
				declared[route.Method+" "+route.Path] = true
			}
			checkRoutes(t, router, declared)
			// Feed operations are mounted by the exchange-rate subsystem.
			if reg.EntityName() == "RateSource" {
				require.Len(t, declared, 3)
			} else {
				require.Empty(t, declared)
			}
		})
	}
	for _, reg := range factories.Documents() {
		t.Run(reg.EntityName(), func(t *testing.T) {
			checkIdentity(reg)
			caps := reg.(v1.DocumentCapabilitiesProvider).DocumentCapabilities()
			router := gin.New()
			v1.RegisterDocumentRoutes(router.Group("/document/"+reg.RoutePrefix()), reg.Build(deps), reg.Permission(), caps)
			declared := map[string]bool{}
			for _, route := range reg.HTTPContracts().Routes {
				declared[route.Method+" "+route.Path] = true
			}
			checkRoutes(t, router, declared)
			require.Empty(t, declared, "wire bundle publishes routes absent from handler")
		})
	}
	for _, reg := range factories.CustomDocuments() {
		t.Run(reg.EntityName(), func(t *testing.T) {
			checkIdentity(reg)
			router := gin.New()
			reg.RegisterRoutes(router.Group("/document/"+reg.RoutePrefix()), deps)
			declared := map[string]bool{}
			for _, route := range reg.HTTPContracts().Routes {
				declared[route.Method+" "+route.Path] = true
			}
			checkRoutes(t, router, declared)
			require.Empty(t, declared)
		})
	}
	require.Len(t, identities, 22)
}

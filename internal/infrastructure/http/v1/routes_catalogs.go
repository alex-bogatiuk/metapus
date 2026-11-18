package v1

import (
	"metapus/internal/infrastructure/http/v1/middleware"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/domain"
	"metapus/internal/domain/eventlog"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/metadata"
	"metapus/internal/platform"
)

// registerCatalogRoutes registers catalog (reference) endpoints via the Abstract Factory registry.
// Also populates the metadata registry and builds refEndpoints map.
func registerCatalogRoutes(rg *gin.RouterGroup, cfg RouterConfig, factoryReg *FactoryRegistry, reg *metadata.Registry, eventWriter eventlog.Writer, currencyInvalidator domain.CurrencyCacheInvalidator) {
	catalogs := rg.Group("/catalog")

	deps := CatalogDeps{
		BaseHandler:              handlers.NewBaseHandler(),
		Numerator:                cfg.Numerator,
		PolicyEngine:             cfg.PolicyEngine,
		EventWriter:              eventWriter,
		CurrencyCacheInvalidator: currencyInvalidator,
	}

	// Build refEndpoints from factory declarations
	refEndpoints := map[string]string{
		"parent": "", // parent is self-referencing, skip
	}
	for _, factory := range factoryReg.Catalogs() {
		if rp, ok := factory.(platform.ReferenceProvider); ok {
			for _, refType := range rp.ReferenceTypes() {
				refEndpoints[refType] = "/catalog/" + factory.RoutePrefix()
			}
		}
	}

	for _, factory := range factoryReg.Catalogs() {
		handler := factory.Build(deps)
		group := catalogs.Group("/" + factory.RoutePrefix())
		if sensitive, ok := factory.(interface{ SensitiveMutationAction() string }); ok {
			guard := middleware.SensitiveActionGuard(cfg.MFARepo, sensitive.SensitiveMutationAction(), cfg.EventLogRepo)
			group.Use(func(c *gin.Context) {
				if c.Request.Method == http.MethodGet || strings.HasSuffix(c.FullPath(), "/export-list") {
					c.Next()
					return
				}
				guard(c)
			})
		}
		RegisterCatalogRoutes(group, handler, factory.Permission())
		if extended, ok := factory.(CatalogSubresourceRegistration); ok {
			extended.RegisterSubresourceRoutes(group, cfg)
		}

		// Register reference mappings: refType → entityName (optional)
		if rp, ok := factory.(platform.ReferenceProvider); ok {
			for _, refType := range rp.ReferenceTypes() {
				reg.RegisterReferenceMapping(refType, factory.EntityName())
			}
		}

		// Auto-register metadata (optional: Inspectable, Presentable)
		var def metadata.EntityDef
		if insp, ok := factory.(platform.Inspectable); ok {
			def = metadata.Inspect(insp.EntityStruct(), factory.EntityName(), metadata.TypeCatalog)
		} else {
			def = metadata.EntityDef{Name: factory.EntityName(), Type: metadata.TypeCatalog}
		}
		if pres, ok := factory.(platform.Presentable); ok {
			def.Presentation = pres.EntityPresentation()
		}
		identity := factory.EntityDefinition()
		def.TableName = identity.Table
		metadata.MustBindParts(&def, identity)
		metadata.BindCode(&def, identity)
		def.Key = identity.Key
		def.ReadPermission = identity.Permission() + ":read"
		def.RoutePrefix = identity.Route

		def.SetRefEndpoints(refEndpoints)
		if rls, ok := factory.(platform.RLSProvider); ok {
			def.RLSDimensions = rls.RLSDimensions()
		}
		if sf, ok := searchableFields(factory); ok {
			fields := sf
			def.SearchColumns = &metadata.SearchColumns{
				SearchCols:  fields.SearchCols,
				TitleCol:    fields.TitleCol,
				SubtitleCol: fields.SubtitleCol,
			}
		}
		reg.Register(def)
	}
}

// searchableFields returns the global-search columns of a catalog: those of a
// SearchFieldsProvider, or those a declared catalog set with Search.
func searchableFields(factory CatalogRegistration) (platform.SearchFields, bool) {
	if sf, ok := factory.(platform.SearchFieldsProvider); ok {
		return sf.SearchableFields(), true
	}
	if auto, ok := factory.(autoSearchable); ok && auto.searchFields() != nil {
		return *auto.searchFields(), true
	}
	return platform.SearchFields{}, false
}

// Package v1 registers a configuration's entities and routes in the
// platform's HTTP API.
package v1

import (
	"github.com/gin-gonic/gin"

	"metapus/internal/core/entity"
	core "metapus/internal/infrastructure/http/v1"
	"metapus/internal/metadata"
	"metapus/pkg/sdk/registry"
)

type (
	// CatalogDeps holds shared dependencies injected into every catalog factory.
	CatalogDeps = core.CatalogDeps
	// CatalogRegistration is the Abstract Factory interface for catalog types.
	// This is the REQUIRED contract — all methods must be implemented.
	//
	// Optional interfaces (checked via type assertion — see internal/platform/):
	//   - platform.Presentable      — EntityPresentation() metadata.Presentation
	//   - platform.Inspectable      — EntityStruct() interface{}
	//   - platform.Labeled           — EntityLabel() string
	//   - platform.ReferenceProvider — ReferenceTypes() []string
	//
	// Adding a new catalog type:
	//  1. Create model, repo, service (embed CatalogService).
	//  2. Create handler (CatalogHandler[T, CreateDTO, UpdateDTO]).
	//  3. Implement CatalogRegistration (+ optional interfaces for metadata).
	//  4. Register via FactoryRegistry (see factory_registry.go).
	CatalogRegistration = core.CatalogRegistration
	// CatalogRegistrationFor shares standard typed DTOs and routes between built-in
	// entities and extensions. Build remains the entity's explicit composition root.
	CatalogRegistrationFor[T entity.DefinitionProvider, Create any, Update any, Read any] = core.CatalogRegistrationFor[T, Create, Update, Read]
	// CatalogRouteHandler defines the interface for catalog handlers.
	// All catalog handlers must implement these methods.
	CatalogRouteHandler = core.CatalogRouteHandler
	// CustomDocumentRegistration registers document-like workflows that need
	// metadata and document-scoped routes, but intentionally do not expose the
	// standard CRUD/post/unpost route set.
	CustomDocumentRegistration = core.CustomDocumentRegistration
	// DocumentActionsProvider exposes explicit custom document actions in metadata.
	DocumentActionsProvider = core.DocumentActionsProvider
	// DocumentCapabilitiesProvider lets a document registration override the
	// standard handler-derived capability set.
	DocumentCapabilitiesProvider = core.DocumentCapabilitiesProvider
	// DocumentDeps holds shared dependencies injected into every document factory.
	DocumentDeps = core.DocumentDeps
	// DocumentRegistration is the Abstract Factory interface for document types.
	// This is the REQUIRED contract — all methods must be implemented.
	//
	// Optional interfaces (checked via type assertion — see internal/platform/):
	//   - platform.Presentable      — EntityPresentation() metadata.Presentation
	//   - platform.Inspectable      — EntityStruct() interface{}
	//   - platform.Labeled           — EntityLabel() string
	//
	// Adding a new document type:
	//  1. Create model, repo, service (embed BaseDocumentService).
	//  2. Create handler (embed BaseDocumentHandler).
	//  3. Implement DocumentRegistration (+ optional interfaces for metadata).
	//  4. Register via FactoryRegistry (see factory_registry.go).
	DocumentRegistration = core.DocumentRegistration
	// DocumentRepostHandler is an optional interface for documents that support
	// atomic update-and-repost (1C-style "Write and post document").
	DocumentRepostHandler = core.DocumentRepostHandler
	// DocumentRouteHandler defines the interface for document handlers.
	// All document handlers must implement these methods.
	DocumentRouteHandler = core.DocumentRouteHandler
	// EntityRegistration derives transport identity from the domain model. Its zero
	// value is ready to use, so registrations cannot forget to initialize identity.
	EntityRegistration[T entity.DefinitionProvider] = core.EntityRegistration[T]
	// FactoryRegistry is the extensible registry of all entity factories.
	// Clients add their own factories via Register methods without modifying core files.
	//
	// Usage (composition root / main.go):
	//
	//	reg := configuration.Compose(cfg, extensions.Register) // the configuration's entities, then client extensions
	//
	//	router := v1.NewRouter(v1.RouterConfig{
	//	    Registry: reg,
	//	    // ...
	//	})
	FactoryRegistry = core.FactoryRegistry
	// PrintFormsProvider declares the print forms of a document type. The router
	// registers them under the document's definition key before building handlers.
	PrintFormsProvider = core.PrintFormsProvider
	// RouteContext exposes the platform router groups and shared dependencies.
	RouteContext = core.RouteContext
	// RouteModule mounts one configuration's routes on the platform router.
	// Modules run after the platform routes are registered, so Metadata is complete.
	RouteModule = core.RouteModule
	// RouterConfig holds router configuration for multi-tenant architecture.
	RouterConfig = core.RouterConfig
)

func BuildReadRegistry(factories *core.FactoryRegistry) *metadata.Registry {
	return core.BuildReadRegistry(factories)
}

// Catalog declares a catalog from its model T, which embeds entity.Catalog
// and returns its Definition from EntityDefinition.
func Catalog[T entity.DefinitionProvider, PT registry.CatalogModel[T]](presentation metadata.Presentation) *core.AutoCatalog[T, PT] {
	return registry.Catalog[T, PT](presentation)
}

// RegisterCatalogRoutes registers standard CRUD routes for a catalog.
// This eliminates the need to manually wire up routes for each catalog.
//
// Usage:
//
//	repo := catalog_repo.NewCurrencyRepo(cfg.TxManager, cfg.Isolation)
//	service := currency.NewService(repo, cfg.TxManager, cfg.Numerator)
//	handler := handlers.NewCurrencyHandler(baseHandler, service)
//	RegisterCatalogRoutes(catalogs.Group("/currencies"), handler, "catalog:currency")
func RegisterCatalogRoutes(group *gin.RouterGroup, handler core.CatalogRouteHandler, permission string) {
	core.RegisterCatalogRoutes(group, handler, permission)
}

// RegisterDocumentRoutes registers the capability-enabled generic document route set.
// If a capability is false, the corresponding standard route is not registered.
// Optional handler routes also require both the capability and the optional
// handler interface to be present.
//
// Usage:
//
//	repo := document_repo.NewCryptoInvoiceRepo(cfg.TxManager, cfg.Isolation)
//	service := crypto_invoice.NewService(repo, cfg.TxManager, postingEngine, cfg.Numerator)
//	handler := handlers.NewCryptoInvoiceHandler(baseHandler, service)
//	RegisterDocumentRoutes(documents.Group("/crypto-invoices"), handler, "document:crypto_invoice", InferDocumentCapabilities(handler))
func RegisterDocumentRoutes(group *gin.RouterGroup, handler core.DocumentRouteHandler, permission string, caps metadata.DocumentCapabilities) {
	core.RegisterDocumentRoutes(group, handler, permission, caps)
}

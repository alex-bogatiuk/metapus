// Package v1 provides HTTP API version 1.
// catalog_factory.go — Core interfaces for catalog registration (CORE).
// Concrete registrations live in catalog_registrations.go (BUSINESS CONTENT).
package v1

import (
	"metapus/internal/core/eventlog"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/http/v1/handlers"
)

// CatalogDeps holds shared dependencies injected into every catalog factory.
type CatalogDeps struct {
	BaseHandler              *handlers.BaseHandler
	Numerator                numerator.Generator
	PolicyEngine             *security.PolicyEngine
	EventWriter              eventlog.Writer                // optional — nil disables event logging
	CurrencyCacheInvalidator domain.CurrencyCacheInvalidator // optional — nil when no currency caching
}

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
type CatalogRegistration interface {
	// RoutePrefix returns the URL path segment, e.g. "counterparties".
	RoutePrefix() string
	// Permission returns the permission prefix, e.g. "catalog:counterparty".
	Permission() string
	// EntityName returns the metadata registry name, e.g. "Counterparty".
	EntityName() string
	// Build creates repo, service, and handler.
	Build(deps CatalogDeps) CatalogRouteHandler
}

// NOTE: Catalog factories are now registered via FactoryRegistry (see factory_registry.go).
// Use content.RegisterDefaults() to populate built-in catalogs, or register custom ones:
//
//	reg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(reg)
//	reg.RegisterCatalog(&custom.MyCatalogRegistration{})
//
// Optional interface examples (implement for richer metadata):
//
//	func (r *MyReg) EntityPresentation() metadata.Presentation { ... }  // platform.Presentable
//	func (r *MyReg) EntityStruct() interface{} { return MyModel{} }     // platform.Inspectable
//	func (r *MyReg) EntityLabel() string { return "My Catalogs" }       // platform.Labeled
//	func (r *MyReg) ReferenceTypes() []string { return []string{"my"} } // platform.ReferenceProvider
//
// Concrete registrations: see catalog_registrations.go.

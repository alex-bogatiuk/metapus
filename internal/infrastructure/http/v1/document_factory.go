// Package v1 provides HTTP API version 1.
// document_factory.go — Core interfaces for document registration (CORE).
// Concrete registrations live in document_registrations.go (BUSINESS CONTENT).
package v1

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/eventlog"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/v1/handlers"
)

// DocumentDeps holds shared dependencies injected into every document factory.
type DocumentDeps struct {
	BaseHandler      *handlers.BaseHandler
	PostingEngine    *posting.Engine
	Numerator        numerator.Generator
	CurrencyResolver domain.CurrencyResolveStrategy
	CurrencyMetadataResolver domain.CurrencyMetadataResolver // Added for automation outbox
	PolicyEngine     *security.PolicyEngine
	EventWriter      eventlog.Writer // optional — nil disables event logging
	OutboxPublisher  domain.OutboxPublisher // optional — nil disables outbox events
	PrintRegistry    *printing.PrintFormRegistry
	PrintRenderer    *printing.Renderer      // nil disables print route
	RelatedDocFinder domain.RelatedDocFinder // optional — nil disables related documents route

	// MovementProviders allow cross-register movement rendering
	MovementProviders   []entity.MovementProvider
	MovementRefResolver domain.RefResolver

	// SettingsRepo provides tenant-level settings (batch concurrency, etc.).
	// If nil, default values are used in handlers.
	SettingsRepo settings.Repository
}

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
type DocumentRegistration interface {
	// RoutePrefix returns the URL path segment, e.g. "goods-receipt".
	RoutePrefix() string
	// Permission returns the permission prefix, e.g. "document:goods_receipt".
	Permission() string
	// EntityName returns the metadata registry name, e.g. "GoodsReceipt".
	EntityName() string
	// Build creates repo, service (with audit hooks), and handler.
	Build(deps DocumentDeps) DocumentRouteHandler
}

// NOTE: Document factories are now registered via FactoryRegistry (see factory_registry.go).
// Use content.RegisterDefaults() to populate built-in documents, or register custom ones:
//
//	reg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(reg)
//	reg.RegisterDocument(&custom.MyDocRegistration{})
//
// Concrete registrations: see document_registrations.go.

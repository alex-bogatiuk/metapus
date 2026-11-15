// Package v1 provides HTTP API version 1.
// document_factory.go — Core interfaces for document registration (CORE).
// Concrete registrations live in document_registrations.go (BUSINESS CONTENT).
package v1

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	"metapus/internal/domain/eventlog"
	"metapus/internal/domain/posting"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/settings"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/http/v1/middleware"
	printadapter "metapus/internal/infrastructure/printing"
	"metapus/internal/metadata"

	"github.com/gin-gonic/gin"
)

// DocumentDeps holds shared dependencies injected into every document factory.
type DocumentDeps struct {
	BaseHandler              *handlers.BaseHandler
	PostingEngine            *posting.Engine
	Numerator                numerator.Generator
	CurrencyResolver         domain.CurrencyResolveStrategy
	CurrencyMetadataResolver domain.CurrencyMetadataResolver // Added for automation outbox
	PolicyEngine             *security.PolicyEngine
	EventWriter              eventlog.Writer        // optional — nil disables event logging
	OutboxPublisher          domain.OutboxPublisher // optional — nil disables outbox events
	PrintRegistry            *printing.PrintFormRegistry
	PrintRenderer            *printadapter.Renderer // nil: print requests fail with an internal error
	PDFRenderer              printing.PDFRenderer
	RelatedDocFinder         domain.RelatedDocFinder // optional — nil disables related documents route
	StepUpGuard              func(actionCode string) gin.HandlerFunc
	StepUpChecker            middleware.StepUpChecker

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
	entity.DefinitionProvider
	spec.Provider
	// RoutePrefix returns the URL path segment, e.g. "crypto-invoices".
	RoutePrefix() string
	// Permission returns the permission prefix, e.g. "document:crypto_invoice".
	Permission() string
	// EntityName returns the metadata registry name, e.g. "CryptoInvoice".
	EntityName() string
	// Build creates repo, service (with audit hooks), and handler.
	Build(deps DocumentDeps) DocumentRouteHandler
}

// CustomDocumentRegistration registers document-like workflows that need
// metadata and document-scoped routes, but intentionally do not expose the
// standard CRUD/post/unpost route set.
type CustomDocumentRegistration interface {
	entity.DefinitionProvider
	spec.Provider
	// RoutePrefix returns the URL path segment, e.g. "withdrawal-request".
	RoutePrefix() string
	// Permission returns the permission prefix, e.g. "document:withdrawal_request".
	Permission() string
	// EntityName returns the metadata registry name, e.g. "WithdrawalRequest".
	EntityName() string
	// RegisterRoutes mounts the custom document workflow routes on the provided group.
	RegisterRoutes(group *gin.RouterGroup, deps DocumentDeps)
}

// DocumentCapabilitiesProvider lets a document registration override the
// standard handler-derived capability set.
type DocumentCapabilitiesProvider interface {
	DocumentCapabilities() metadata.DocumentCapabilities
}

// DocumentActionsProvider exposes explicit custom document actions in metadata.
type DocumentActionsProvider interface {
	DocumentActions() []metadata.EntityActionDef
}

// NOTE: Document factories are now registered via FactoryRegistry (see factory_registry.go).
// Use content.RegisterDefaults() to populate built-in documents, or register custom ones:
//
//	reg := v1.NewFactoryRegistry()
//	content.RegisterDefaults(reg)
//	reg.RegisterDocument(&custom.MyDocRegistration{})
//
// Concrete registrations: see document_registrations.go.

// Package handlers are the platform's HTTP handlers a configuration builds
// its own on.
package handlers

import (
	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain"

	"github.com/alex-bogatiuk/metapus/internal/domain/settings"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/handlers"
)

type (

	// BaseDocumentHandler provides generic HTTP handlers for document entities.
	// In Database-per-Tenant architecture, tenantID is not needed (isolation is physical).
	BaseDocumentHandler[T any, CreateDTO any, UpdateDTO any] = core.BaseDocumentHandler[T, CreateDTO, UpdateDTO]
	// BaseDocumentHandlerConfig configures the document handler.
	BaseDocumentHandlerConfig[T any, CreateDTO any, UpdateDTO any] = core.BaseDocumentHandlerConfig[T, CreateDTO, UpdateDTO]
	// BaseHandler provides common handler utilities.
	BaseHandler = core.BaseHandler
	// CatalogHandlerConfig configures the catalog handler.
	CatalogHandlerConfig[T entity.CatalogEntity, CreateDTO any, UpdateDTO any] = core.CatalogHandlerConfig[T, CreateDTO, UpdateDTO]
	// DocumentPrintHandler provides the print HTTP handlers for a single document type.
	// A document handler keeps it in a named field and delegates Print and
	// ListPrintForms to it (see CryptoInvoiceHandler); embedding would make the
	// shared BaseHandler methods ambiguous. The routes exist only when the
	// registration also declares the PrintForms capability.
	DocumentPrintHandler[T any] = core.DocumentPrintHandler[T]
	// DocumentPrintHandlerConfig configures a document print handler.
	DocumentPrintHandlerConfig[T any] = core.DocumentPrintHandlerConfig[T]
	// ReadOnlyDocumentPresenter maps a system-owned document to its response DTO.
	// Collect registers the references the DTO displays; Present builds the DTO.
	// Enrich, when set, adds what the current reader may do to the opened card.
	ReadOnlyDocumentPresenter[T any, R any] = core.ReadOnlyDocumentPresenter[T, R]
	// RelatedDocumentsHandler handles GET /:id/related-documents requests.
	// Mounted on each document type's route group automatically.
	RelatedDocumentsHandler = core.RelatedDocumentsHandler
)

const (
	// ExportMaxRows is the safety limit for list export.
	// Prevents OOM on large datasets. For larger exports, use background reports.
	ExportMaxRows = core.ExportMaxRows
)

// DTOToMap converts a DTO (any struct/map) to map[string]any via JSON roundtrip.
// This is the simplest approach for extracting field values by key.
func DTOToMap(v any) (map[string]any, error) { return core.DTOToMap(v) }

// NewBaseDocumentHandler creates a new base document handler.
func NewBaseDocumentHandler[T any, CreateDTO any, UpdateDTO any](base *core.BaseHandler, cfg core.BaseDocumentHandlerConfig[T, CreateDTO, UpdateDTO]) *core.BaseDocumentHandler[T, CreateDTO, UpdateDTO] {
	return core.NewBaseDocumentHandler[T, CreateDTO, UpdateDTO](base, cfg)
}

// NewBaseHandler creates a new base handler.
func NewBaseHandler() *core.BaseHandler { return core.NewBaseHandler() }

// NewCatalogHandler creates a new catalog handler.
func NewCatalogHandler[T entity.CatalogEntity, CreateDTO any, UpdateDTO any](base *core.BaseHandler, cfg core.CatalogHandlerConfig[T, CreateDTO, UpdateDTO]) *core.CatalogHandler[T, CreateDTO, UpdateDTO] {
	return core.NewCatalogHandler[T, CreateDTO, UpdateDTO](base, cfg)
}

// NewDocumentPrintHandler creates a DocumentPrintHandler with the given config.
func NewDocumentPrintHandler[T any](base *core.BaseHandler, cfg core.DocumentPrintHandlerConfig[T]) *core.DocumentPrintHandler[T] {
	return core.NewDocumentPrintHandler[T](base, cfg)
}

// NewReadOnlyDocumentHandler serves system-owned documents that are created
// and decided only through their own workflows: list, card and register
// movements. Create and update routes are not mounted for them.
func NewReadOnlyDocumentHandler[T any, R any](base *core.BaseHandler, service domain.DocumentService[T], entityName string, presenter core.ReadOnlyDocumentPresenter[T, R], movementProviders []entity.MovementProvider, movementRefResolver domain.RefResolver, settingsRepo settings.Repository) *core.BaseDocumentHandler[T, struct{}, struct{}] {
	return core.NewReadOnlyDocumentHandler[T, R](base, service, entityName, presenter, movementProviders, movementRefResolver, settingsRepo)
}

// NewRelatedDocumentsHandler creates a new handler.
func NewRelatedDocumentsHandler(finder domain.RelatedDocFinder, entityName string) *core.RelatedDocumentsHandler {
	return core.NewRelatedDocumentsHandler(finder, entityName)
}

// ParseExportRequest parses and validates an ExportListRequest from the gin context.
func ParseExportRequest(c *gin.Context) (dto.ExportListRequest, domain.ListFilter, error) {
	return core.ParseExportRequest(c)
}

// WriteExportXLSX builds the XLSX from DTO items and writes it to the response.
func WriteExportXLSX(c *gin.Context, title string, req dto.ExportListRequest, dtoItems []any) {
	core.WriteExportXLSX(c, title, req, dtoItems)
}

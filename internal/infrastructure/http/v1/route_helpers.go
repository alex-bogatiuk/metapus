// Package v1 provides HTTP API version 1.
package v1

import (
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/middleware"
	"github.com/alex-bogatiuk/metapus/internal/metadata"

	"github.com/gin-gonic/gin"
)

// CatalogRouteHandler defines the interface for catalog handlers.
// All catalog handlers must implement these methods.
type CatalogRouteHandler interface {
	List(c *gin.Context)
	Create(c *gin.Context)
	Get(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
	SetDeletionMark(c *gin.Context)
	GetTree(c *gin.Context)
}

// DocumentRouteHandler defines the interface for document handlers.
// All document handlers must implement these methods.
type DocumentRouteHandler interface {
	List(c *gin.Context)
	Create(c *gin.Context)
	Get(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
	Post(c *gin.Context)
	Unpost(c *gin.Context)
	SetDeletionMark(c *gin.Context)
}

// DocumentCopyHandler is an optional interface for documents that support copying.
type DocumentCopyHandler interface {
	Copy(c *gin.Context)
}

// DocumentPrintHandlerInterface is an optional interface for documents that support print forms.
// When a handler implements this interface, RegisterDocumentRoutes automatically adds
// GET /:id/print?format=<name> requiring the entity read permission.
type DocumentPrintHandlerInterface interface {
	Print(c *gin.Context)
}

// DocumentPrintFormsListHandler is an optional interface for listing available print forms.
// When a handler implements this interface, RegisterDocumentRoutes automatically adds
// GET /print-forms requiring the entity read permission.
type DocumentPrintFormsListHandler interface {
	ListPrintForms(c *gin.Context)
}

// DocumentRepostHandler is an optional interface for documents that support
// atomic update-and-repost (1C-style "Write and post document").
type DocumentRepostHandler interface {
	UpdateAndRepost(c *gin.Context)
}

// DocumentRelatedDocsHandler is an optional interface for documents that support
// "Related Documents" (Связанные документы) sidebar.
// When a handler implements this interface, RegisterDocumentRoutes automatically adds
// GET /:id/related-documents requiring the entity read permission.
type DocumentRelatedDocsHandler interface {
	GetRelatedDocuments(c *gin.Context)
}

// DocumentMovementsHandlerInterface is an optional interface for documents that support
// "Movements" (Движения) feature.
// When a handler implements this interface, RegisterDocumentRoutes automatically adds
// GET /:id/movements requiring the entity read permission.
type DocumentMovementsHandlerInterface interface {
	GetMovements(c *gin.Context)
}

// DocumentBatchHandler is an optional interface for batch operations.
// When a handler implements this interface, RegisterDocumentRoutes automatically adds
// POST /batch-action requiring the entity post permission.
type DocumentBatchHandler interface {
	BatchAction(c *gin.Context)
}

// DocumentBatchByFilterHandler is an optional interface for filter-based batch operations.
// When a handler implements this interface, RegisterDocumentRoutes automatically adds
// POST /batch-action-by-filter requiring the entity post permission.
// Used for virtual "select all" — the server resolves matching IDs from filters.
type DocumentBatchByFilterHandler interface {
	BatchActionByFilter(c *gin.Context)
}

// DocumentCustomRoutesHandler is an optional interface for document-specific
// workflow endpoints that still belong under /document/{routePrefix}.
type DocumentCustomRoutesHandler interface {
	RegisterCustomDocumentRoutes(group *gin.RouterGroup, permission string)
}

// ListExportHandler is an optional interface for exporting a list to XLSX.
// When a handler implements this interface, RegisterCatalogRoutes / RegisterDocumentRoutes
// automatically adds POST /export-list requiring the entity read permission.
type ListExportHandler interface {
	ExportList(c *gin.Context)
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
func RegisterCatalogRoutes(group *gin.RouterGroup, handler CatalogRouteHandler, permission string) {
	replay := middleware.ReplayPresenter(nil)
	if presenter, ok := handler.(interface {
		ReplayPresenter() middleware.ReplayPresenter
	}); ok {
		replay = presenter.ReplayPresenter()
	}
	idempotency := middleware.CommandIdempotencyWithReplay(false, replay)
	group.GET("", middleware.RequirePermission(permission+":read"), handler.List)
	group.POST("", middleware.RequirePermission(permission+":create"), idempotency, handler.Create)
	group.GET("/:id", middleware.RequirePermission(permission+":read"), handler.Get)
	group.PUT("/:id", middleware.RequirePermission(permission+":update"), idempotency, handler.Update)
	group.DELETE("/:id", middleware.RequirePermission(permission+":delete"), handler.Delete)
	group.POST("/:id/deletion-mark", middleware.RequirePermission(permission+":delete"), handler.SetDeletionMark)
	group.GET("/tree", middleware.RequirePermission(permission+":read"), handler.GetTree)

	// Register ExportList route if handler supports it (optional)
	if exportHandler, ok := handler.(ListExportHandler); ok {
		group.POST("/export-list", middleware.RequirePermission(permission+":read"), exportHandler.ExportList)
	}

}

// InferDocumentCapabilities derives the default generic document surface from
// the required DocumentRouteHandler contract plus optional handler interfaces.
func InferDocumentCapabilities(handler DocumentRouteHandler) metadata.DocumentCapabilities {
	caps := metadata.DocumentCapabilities{
		List:            true,
		Read:            true,
		Create:          true,
		Update:          true,
		Delete:          true,
		SetDeletionMark: true,
		Post:            true,
		Unpost:          true,
	}
	if _, ok := handler.(DocumentCopyHandler); ok {
		caps.Copy = true
	}
	if _, ok := handler.(DocumentRepostHandler); ok {
		caps.Repost = true
	}
	if _, ok := handler.(DocumentPrintFormsListHandler); ok {
		caps.PrintForms = true
	}
	if _, ok := handler.(DocumentRelatedDocsHandler); ok {
		caps.RelatedDocuments = true
	}
	if _, ok := handler.(DocumentMovementsHandlerInterface); ok {
		caps.Movements = true
	}
	if _, ok := handler.(DocumentBatchHandler); ok {
		caps.BatchAction = true
	}
	if _, ok := handler.(DocumentBatchByFilterHandler); ok {
		caps.BatchActionByFilter = true
	}
	if _, ok := handler.(ListExportHandler); ok {
		caps.ExportList = true
	}
	return caps
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
func RegisterDocumentRoutes(group *gin.RouterGroup, handler DocumentRouteHandler, permission string, caps metadata.DocumentCapabilities) {
	if guarded, ok := handler.(interface{ RequiresCommandIdempotency() bool }); ok && guarded.RequiresCommandIdempotency() {
		group.Use(middleware.CommandIdempotency(true))
	}
	if caps.List {
		group.GET("", middleware.RequirePermission(permission+":read"), handler.List)
	}
	if caps.Create {
		group.POST("", middleware.RequirePermission(permission+":create"), handler.Create)
	}
	if caps.Read {
		group.GET("/:id", middleware.RequirePermission(permission+":read"), handler.Get)
	}
	if caps.Update {
		group.PUT("/:id", middleware.RequirePermission(permission+":update"), handler.Update)
	}
	if caps.Delete {
		group.DELETE("/:id", middleware.RequirePermission(permission+":delete"), handler.Delete)
	}
	if caps.Post {
		group.POST("/:id/post", middleware.RequirePermission(permission+":post"), handler.Post)
	}
	if caps.Unpost {
		group.POST("/:id/unpost", middleware.RequirePermission(permission+":unpost"), handler.Unpost)
	}
	if caps.SetDeletionMark {
		group.POST("/:id/deletion-mark", middleware.RequirePermission(permission+":delete"), handler.SetDeletionMark)
	}

	// Register Copy route if handler supports it (optional)
	if copyHandler, ok := handler.(DocumentCopyHandler); ok && caps.Copy {
		group.POST("/:id/copy", middleware.RequirePermission(permission+":create"), copyHandler.Copy)
	}

	// Register Repost route if handler supports it (optional)
	if repostHandler, ok := handler.(DocumentRepostHandler); ok && caps.Repost {
		group.PUT("/:id/repost", middleware.RequirePermission(permission+":post"), repostHandler.UpdateAndRepost)
	}

	// Register Print route if handler supports it (optional)
	if printHandler, ok := handler.(DocumentPrintHandlerInterface); ok && caps.PrintForms {
		group.GET("/:id/print", middleware.RequirePermission(permission+":read"), printHandler.Print)
	}

	// Register Print Forms List route if handler supports it (optional)
	if listFormsHandler, ok := handler.(DocumentPrintFormsListHandler); ok && caps.PrintForms {
		group.GET("/print-forms", middleware.RequirePermission(permission+":read"), listFormsHandler.ListPrintForms)
	}

	// Register Related Documents route if handler supports it (optional)
	if relatedHandler, ok := handler.(DocumentRelatedDocsHandler); ok && caps.RelatedDocuments {
		group.GET("/:id/related-documents", middleware.RequirePermission(permission+":read"), relatedHandler.GetRelatedDocuments)
	}

	// Register Movements route if handler supports it (optional)
	if movHandler, ok := handler.(DocumentMovementsHandlerInterface); ok && caps.Movements {
		group.GET("/:id/movements", middleware.RequirePermission(permission+":read"), movHandler.GetMovements)
	}

	// Drafts filled from other documents (ввод на основании): creating one
	// needs the right to create this document and to read the source.
	if basis, ok := handler.(DocumentBasisHandler); ok && caps.Create {
		for _, route := range basis.BasisRoutes() {
			group.GET("/based-on/"+route.SourceKey+"/:id",
				middleware.RequirePermission(permission+":create"),
				middleware.RequirePermission(route.SourcePermission+":read"),
				route.Handler)
		}
	}

	// Register BatchAction route if handler supports it (optional).
	// Mounted on /batch-action (no :id) — permission checked per-action inside handler.
	if batchHandler, ok := handler.(DocumentBatchHandler); ok && caps.BatchAction {
		group.POST("/batch-action", middleware.RequirePermission(permission+":post"), batchHandler.BatchAction)
	}

	// Register BatchActionByFilter route if handler supports it (optional).
	// Used for virtual "select all" — the server resolves matching IDs from filters.
	if batchFilterHandler, ok := handler.(DocumentBatchByFilterHandler); ok && caps.BatchActionByFilter {
		group.POST("/batch-action-by-filter", middleware.RequirePermission(permission+":post"), batchFilterHandler.BatchActionByFilter)
	}

	// Register ExportList route if handler supports it (optional)
	if exportHandler, ok := handler.(ListExportHandler); ok && caps.ExportList {
		group.POST("/export-list", middleware.RequirePermission(permission+":read"), exportHandler.ExportList)
	}

	if customRoutesHandler, ok := handler.(DocumentCustomRoutesHandler); ok {
		customRoutesHandler.RegisterCustomDocumentRoutes(group, permission)
	}
}

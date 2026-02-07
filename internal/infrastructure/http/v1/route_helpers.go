// Package v1 provides HTTP API version 1.
package v1

import (
	"github.com/gin-gonic/gin"
	"metapus/internal/infrastructure/http/v1/middleware"
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
	group.GET("", middleware.RequirePermission(permission+":read"), handler.List)
	group.POST("", middleware.RequirePermission(permission+":create"), handler.Create)
	group.GET("/:id", middleware.RequirePermission(permission+":read"), handler.Get)
	group.PUT("/:id", middleware.RequirePermission(permission+":update"), handler.Update)
	group.DELETE("/:id", middleware.RequirePermission(permission+":delete"), handler.Delete)
	group.POST("/:id/deletion-mark", middleware.RequirePermission(permission+":delete"), handler.SetDeletionMark)
	group.GET("/tree", middleware.RequirePermission(permission+":read"), handler.GetTree)
}

// RegisterDocumentRoutes registers standard CRUD + posting routes for a document.
// This eliminates the need to manually wire up routes for each document type.
// If the handler also implements DocumentCopyHandler, the Copy route will be registered automatically.
//
// Usage:
//
//	repo := document_repo.NewGoodsReceiptRepo(cfg.TxManager, cfg.Isolation)
//	service := goods_receipt.NewService(repo, cfg.TxManager, postingEngine, cfg.Numerator)
//	handler := handlers.NewGoodsReceiptHandler(baseHandler, service)
//	RegisterDocumentRoutes(documents.Group("/goods-receipt"), handler, "document:goods_receipt")
func RegisterDocumentRoutes(group *gin.RouterGroup, handler DocumentRouteHandler, permission string) {
	group.GET("", middleware.RequirePermission(permission+":read"), handler.List)
	group.POST("", middleware.RequirePermission(permission+":create"), handler.Create)
	group.GET("/:id", middleware.RequirePermission(permission+":read"), handler.Get)
	group.PUT("/:id", middleware.RequirePermission(permission+":update"), handler.Update)
	group.DELETE("/:id", middleware.RequirePermission(permission+":delete"), handler.Delete)
	group.POST("/:id/post", middleware.RequirePermission(permission+":post"), handler.Post)
	group.POST("/:id/unpost", middleware.RequirePermission(permission+":unpost"), handler.Unpost)
	group.POST("/:id/deletion-mark", middleware.RequirePermission(permission+":delete"), handler.SetDeletionMark)

	// Register Copy route if handler supports it (optional)
	if copyHandler, ok := handler.(DocumentCopyHandler); ok {
		group.POST("/:id/copy", middleware.RequirePermission(permission+":create"), copyHandler.Copy)
	}
}

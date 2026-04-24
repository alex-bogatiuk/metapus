package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/domain/reports/compiler"
	"metapus/internal/domain/reports/export"
	"metapus/internal/metadata"
)

// ---------------------------------------------------------------------------
// Dataset Report Handler (Query Engine)
// ---------------------------------------------------------------------------
// Handles dataset-based reports via the Compiler.
// Replaces the per-report executor pattern with a single generic handler.

// DatasetReportHandler serves dataset-based report endpoints.
type DatasetReportHandler struct {
	*BaseHandler
	compiler *compiler.Compiler
	registry *metadata.Registry
}

// NewDatasetReportHandler creates a handler for dataset-based reports.
func NewDatasetReportHandler(base *BaseHandler, comp *compiler.Compiler, reg *metadata.Registry) *DatasetReportHandler {
	return &DatasetReportHandler{
		BaseHandler: base,
		compiler:    comp,
		registry:    reg,
	}
}

// HandleMeta returns a gin.HandlerFunc that serves GET /reports/{key}/metadata.
func (h *DatasetReportHandler) HandleMeta(datasetKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ds := h.compiler.GetDataset(datasetKey)
		if ds == nil {
			h.Error(c, apperror.NewNotFound("dataset", datasetKey))
			return
		}
		meta := compiler.DatasetToMeta(ds, h.registry)
		c.JSON(http.StatusOK, meta)
	}
}

// HandleExecute serves POST /reports/{key} → execute report via Compiler.
func (h *DatasetReportHandler) HandleExecute(c *gin.Context) {
	ctx := c.Request.Context()

	var req compiler.QueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.Error(c, apperror.NewValidation("invalid request body").WithDetail("error", err.Error()))
		return
	}

	result, err := h.compiler.Execute(ctx, req)
	if err != nil {
		h.Error(c, apperror.NewInternal(err))
		return
	}

	c.JSON(http.StatusOK, result)
}

// HandleExport returns a gin.HandlerFunc that serves POST /reports/{key}/export.
func (h *DatasetReportHandler) HandleExport(datasetKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()


		var req compiler.QueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			h.Error(c, apperror.NewValidation("invalid request body").WithDetail("error", err.Error()))
			return
		}
		req.Dataset = datasetKey
		req.Limit = 0 // no limit for export

		result, err := h.compiler.Execute(ctx, req)
		if err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}

		ds := h.compiler.GetDataset(datasetKey)
		meta := compiler.DatasetToMeta(ds, h.registry)

		filename := fmt.Sprintf("%s.xlsx", meta.Key)
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		if err := export.XLSX(c.Writer, meta, result.Items, req.ExportColumns, req.ExportGroupBy); err != nil {
			_ = c.Error(err)
			return
		}
	}
}

// HandleGrouped returns a gin.HandlerFunc that serves POST /reports/{key}/grouped.
func (h *DatasetReportHandler) HandleGrouped(datasetKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var req compiler.QueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			h.Error(c, apperror.NewValidation("invalid request body").WithDetail("error", err.Error()))
			return
		}
		req.Dataset = datasetKey

		result, err := h.compiler.Execute(ctx, req)
		if err != nil {
			h.Error(c, apperror.NewInternal(err))
			return
		}

		ds := h.compiler.GetDataset(datasetKey)
		meta := compiler.DatasetToMeta(ds, h.registry)

		// Parse groupBy and sort from query params
		groupByKeys := c.QueryArray("groupBy")
		sortBy := c.Query("sortBy")
		sortDir := c.DefaultQuery("sortDir", "asc")

		items := result.Items

		// Apply sorting
		if sortBy != "" {
			items = compiler.SortItems(items, sortBy, sortDir)
		}

		// Build grouped display rows
		displayRows := compiler.BuildDisplayRows(items, groupByKeys, meta.Totals)

		c.JSON(http.StatusOK, gin.H{
			"rows":       displayRows,
			"totalItems": len(items),
		})
	}
}

package handlers

import (
	"bytes"
	"fmt"
	"metapus/internal/infrastructure/http/v1/dto"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/infrastructure/http/jsoncodec"
	"metapus/internal/infrastructure/reporting/compiler"
	"metapus/internal/infrastructure/reporting/export"
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
func (h *DatasetReportHandler) HandleExecute(datasetKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var req compiler.QueryRequest
		if err := jsoncodec.DecodeRequestPreserveNumbers(c.Request.Body, &req); err != nil {
			h.Error(c, apperror.NewInvalidRequest(err))
			return
		}

		if !bindDatasetKey(c, &req, datasetKey) {
			return
		}
		result, err := h.compiler.Execute(ctx, req)
		if err != nil {
			h.Error(c, err)
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

// HandleExport returns a gin.HandlerFunc that serves POST /reports/{key}/export.
func (h *DatasetReportHandler) HandleExport(datasetKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var req compiler.QueryRequest
		if err := jsoncodec.DecodeRequestPreserveNumbers(c.Request.Body, &req); err != nil {
			h.Error(c, apperror.NewInvalidRequest(err))
			return
		}
		if !bindDatasetKey(c, &req, datasetKey) {
			return
		}
		req.Limit = 0 // no limit for export

		result, err := h.compiler.Execute(ctx, req)
		if err != nil {
			h.Error(c, err)
			return
		}

		ds := h.compiler.GetDataset(datasetKey)
		meta := compiler.DatasetToMeta(ds, h.registry)

		format := c.DefaultQuery("format", "xlsx")
		var buffer bytes.Buffer
		contentType := "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		switch format {
		case "xlsx":
			err = export.XLSX(&buffer, meta, result.Items, req.ExportColumns, req.ExportGroupBy)
		case "csv":
			contentType = "text/csv; charset=utf-8"
			err = export.CSV(&buffer, meta, result.Items, req.ExportColumns)
		default:
			h.Error(c, apperror.NewValidation("unsupported report export format"))
			return
		}
		if err != nil {
			h.Error(c, apperror.NewValidation("cannot format report").WithCause(err))
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, meta.Key, format))
		c.Data(http.StatusOK, contentType, buffer.Bytes())
	}
}

// HandleGrouped returns a gin.HandlerFunc that serves POST /reports/{key}/grouped.
func (h *DatasetReportHandler) HandleGrouped(datasetKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var req compiler.QueryRequest
		if err := jsoncodec.DecodeRequestPreserveNumbers(c.Request.Body, &req); err != nil {
			h.Error(c, apperror.NewInvalidRequest(err))
			return
		}
		if !bindDatasetKey(c, &req, datasetKey) {
			return
		}

		req.DisplayFields = append(req.DisplayFields, c.QueryArray("groupBy")...)
		if sort := c.Query("sortBy"); sort != "" {
			req.DisplayFields = append(req.DisplayFields, sort)
		}

		result, err := h.compiler.Execute(ctx, req)
		if err != nil {
			h.Error(c, err)
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
		displayRows, err := compiler.BuildDisplayRows(items, groupByKeys, meta.Totals)
		if err != nil {
			h.Error(c, apperror.NewValidation("invalid report totals").WithCause(err))
			return
		}

		c.JSON(http.StatusOK, dto.GroupedReportResponse{Rows: displayRows, TotalItems: len(items)})
	}
}

func bindDatasetKey(c *gin.Context, req *compiler.QueryRequest, key string) bool {
	if req.Dataset != "" && req.Dataset != key {
		err := apperror.NewValidation("body dataset does not match the route")
		err.Code = "DATASET_MISMATCH"
		_ = c.Error(err)
		c.Abort()
		return false
	}
	req.Dataset = key
	return true
}

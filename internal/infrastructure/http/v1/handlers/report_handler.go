package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/security"
	"metapus/internal/domain/reports/compiler"
	"metapus/internal/domain/reports/export"
	"metapus/internal/platform"
)

// ---------------------------------------------------------------------------
// Generic Report Handler
// ---------------------------------------------------------------------------
// Eliminates per-report handler boilerplate. Each report provides:
//   - Meta()    → returned as JSON by GET /{prefix}/metadata
//   - Execute() → called by GET /{prefix} after query-param binding
//   - Export    → called by GET /{prefix}/export?format=csv|xlsx
//
// RLS enforcement: ScopeDimensions from ReportMeta are checked against
// the DataScope in context (populated by SecurityContext middleware).
// If a user's DataScope restricts a dimension that the report declares,
// access is denied before Execute() runs.

// ReportHandler wraps a ReportRouteAdapter and provides gin handlers.
type ReportHandler struct {
	*BaseHandler
	adapter platform.ReportRouteAdapter
}

// NewReportHandler creates a handler for a single report adapter.
func NewReportHandler(base *BaseHandler, adapter platform.ReportRouteAdapter) *ReportHandler {
	return &ReportHandler{BaseHandler: base, adapter: adapter}
}

// HandleMeta serves GET /{prefix}/metadata → report metadata for frontend.
func (h *ReportHandler) HandleMeta(c *gin.Context) {
	c.JSON(http.StatusOK, h.adapter.Meta())
}

// HandleExecute serves GET /{prefix} → execute report with query params.
// Enforces RLS via ScopeDimensions before execution.
func (h *ReportHandler) HandleExecute(c *gin.Context) {
	ctx := c.Request.Context()

	// RLS pre-check: verify user has access to the report's scope dimensions
	if err := h.checkRLSAccess(ctx); err != nil {
		h.Error(c, err)
		return
	}

	queryBinder := func(dst any) error {
		return c.ShouldBindQuery(dst)
	}

	result, err := h.adapter.HandleExecute(ctx, queryBinder)
	if err != nil {
		h.Error(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// HandleExport serves GET /{prefix}/export?format=csv|xlsx.
// Streams the result directly to ResponseWriter — no in-memory buffering
// of the final file (CSV is fully streaming; XLSX builds in excelize then writes).
func (h *ReportHandler) HandleExport(c *gin.Context) {
	ctx := c.Request.Context()

	// RLS pre-check
	if err := h.checkRLSAccess(ctx); err != nil {
		h.Error(c, err)
		return
	}

	format := c.DefaultQuery("format", "csv")
	if format != "csv" && format != "xlsx" {
		h.Error(c, apperror.NewValidation("unsupported export format: "+format))
		return
	}

	queryBinder := func(dst any) error {
		return c.ShouldBindQuery(dst)
	}

	result, err := h.adapter.HandleExecute(ctx, queryBinder)
	if err != nil {
		h.Error(c, err)
		return
	}

	// Convert result to []map[string]interface{} for the export engine
	items, err := resultToMaps(result)
	if err != nil {
		h.Error(c, apperror.NewInternal(fmt.Errorf("failed to prepare export data: %w", err)))
		return
	}

	meta := h.adapter.Meta()

	switch format {
	case "csv":
		filename := fmt.Sprintf("%s.csv", meta.Key)
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		// Write BOM for Excel UTF-8 compatibility
		if _, err := c.Writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			_ = c.Error(err)
			return
		}
		if err := export.CSV(c.Writer, meta, items); err != nil {
			// Already started writing — can't send error JSON.
			// Log and abort.
			_ = c.Error(err)
			return
		}

	case "xlsx":
		filename := fmt.Sprintf("%s.xlsx", meta.Key)
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		if err := export.XLSX(c.Writer, meta, items); err != nil {
			_ = c.Error(err)
			return
		}
	}
}

// HandleGrouped serves GET /{prefix}/grouped?groupBy=col1&groupBy=col2&sortBy=col&sortDir=asc|desc.
// Server-side grouping fallback for datasets > 5000 rows.
// Executes the report, then applies grouping/sorting on the Go side,
// returning pre-built DisplayRow[] identical to what the frontend would compute.
func (h *ReportHandler) HandleGrouped(c *gin.Context) {
	ctx := c.Request.Context()

	if err := h.checkRLSAccess(ctx); err != nil {
		h.Error(c, err)
		return
	}

	queryBinder := func(dst any) error {
		return c.ShouldBindQuery(dst)
	}

	result, err := h.adapter.HandleExecute(ctx, queryBinder)
	if err != nil {
		h.Error(c, err)
		return
	}

	items, err := resultToMaps(result)
	if err != nil {
		h.Error(c, apperror.NewInternal(fmt.Errorf("failed to prepare grouped data: %w", err)))
		return
	}

	meta := h.adapter.Meta()

	// Parse groupBy and sort from query params
	groupByKeys := c.QueryArray("groupBy")
	sortBy := c.Query("sortBy")
	sortDir := c.DefaultQuery("sortDir", "asc")

	// Apply sorting
	if sortBy != "" {
		items = compiler.SortItems(items, sortBy, sortDir)
	}

	// Build grouped display rows (server-side)
	displayRows := compiler.BuildDisplayRows(items, groupByKeys, meta.Totals)

	c.JSON(http.StatusOK, gin.H{
		"rows":       displayRows,
		"totalItems": len(items),
	})
}

// checkRLSAccess verifies the user's DataScope against the report's ScopeDimensions.
// If the report declares ScopeDimensions (e.g. ["warehouse", "organization"])
// and the user's DataScope has restrictions on those dimensions,
// the check passes only if the user has at least one allowed value per dimension.
// Admin users bypass all checks.
func (h *ReportHandler) checkRLSAccess(ctx context.Context) error {
	meta := h.adapter.Meta()
	if len(meta.ScopeDimensions) == 0 {
		return nil // No RLS scope declared
	}

	scope := security.GetDataScope(ctx)
	if scope == nil || scope.IsAdmin {
		return nil // Admin or no security context
	}

	// Check each declared scope dimension
	for _, dim := range meta.ScopeDimensions {
		allowedIDs, hasDimension := scope.Dimensions[dim]
		if !hasDimension {
			// Dimension not restricted in user's scope — no restriction
			continue
		}
		if len(allowedIDs) == 0 {
			// User has the dimension restricted to empty set — deny
			return apperror.NewForbidden(
				fmt.Sprintf("access denied: no permissions for dimension %q", dim),
			)
		}
		// User has at least one allowed value — pass
	}

	return nil
}

// resultToMaps converts the report result (which has an `items` field)
// into []map[string]interface{} for the export engine.
// Uses JSON round-trip for generic conversion (safe since this is export path, not hot path).
func resultToMaps(result any) ([]map[string]interface{}, error) {
	// Marshal to JSON
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	// Unmarshal into a wrapper with items
	var wrapper struct {
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal items: %w", err)
	}

	if wrapper.Items == nil {
		return []map[string]interface{}{}, nil
	}

	return wrapper.Items, nil
}

// ---------------------------------------------------------------------------
// Type-Erased Adapter (bridges generic → non-generic)
// ---------------------------------------------------------------------------

// reportAdapter wraps a typed ReportRegistration[F, R] into a non-generic
// ReportRouteAdapter. This is the bridge between Go generics and the
// FactoryRegistry which stores []RouteRegistration (non-generic).
type reportAdapter[F any, R any] struct {
	report platform.ReportRegistration[F, R]
}

// WrapReportRegistration creates a type-erased adapter from a typed report.
// Called by RegisterTypedReport in factory_registry.go.
func WrapReportRegistration[F any, R any](report platform.ReportRegistration[F, R]) platform.ReportRouteAdapter {
	return &reportAdapter[F, R]{report: report}
}

func (a *reportAdapter[F, R]) RoutePrefix() string {
	return a.report.RoutePrefix()
}

func (a *reportAdapter[F, R]) Permission() string {
	return a.report.Permission()
}

func (a *reportAdapter[F, R]) Meta() platform.ReportMeta {
	meta := a.report.Meta()
	if meta.Key == "" {
		meta.Key = a.report.RoutePrefix()
	}
	return meta
}

// HandleExecute parses query params into F, calls Execute, returns R.
func (a *reportAdapter[F, R]) HandleExecute(ctx context.Context, queryBinder func(dst any) error) (any, error) {
	var filter F
	if err := queryBinder(&filter); err != nil {
		return nil, apperror.NewValidation("invalid report parameters").WithDetail("error", err.Error())
	}

	result, err := a.report.Execute(ctx, filter)
	if err != nil {
		return nil, err
	}

	return result, nil
}


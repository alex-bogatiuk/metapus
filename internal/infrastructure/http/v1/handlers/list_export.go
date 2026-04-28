package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	domainFilter "metapus/internal/domain/filter"
	"metapus/internal/domain/listexport"
	"metapus/internal/infrastructure/http/v1/dto"
)

// ExportMaxRows is the safety limit for list export.
// Prevents OOM on large datasets. For larger exports, use background reports.
const ExportMaxRows = 50_000

// quantityScale matches types.QuantityScale (10_000).
// Duplicated here to avoid importing core/types just for the constant.
const quantityScale = 10_000

// parseExportRequest parses and validates an ExportListRequest from the gin context.
func parseExportRequest(c *gin.Context) (dto.ExportListRequest, domain.ListFilter, error) {
	var req dto.ExportListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return req, domain.ListFilter{}, apperror.NewValidation("invalid request body").WithDetail("error", err.Error())
	}

	if len(req.Columns) == 0 {
		return req, domain.ListFilter{}, apperror.NewValidation("at least one column is required for export")
	}

	// Build ListFilter — same logic as ParseListFilter but from JSON body
	filter := domain.DefaultListFilter()
	filter.Search = req.Search
	filter.IncludeDeleted = req.IncludeDeleted
	filter.SkipCount = true // count is not needed for export
	filter.Limit = ExportMaxRows
	if req.OrderBy != "" {
		filter.OrderBy = req.OrderBy
	}
	// No cursor — export always from the beginning

	// Parse advanced filters from JSON
	if len(req.Filter) > 0 && string(req.Filter) != "null" {
		var advFilters []domainFilter.Item
		if err := json.Unmarshal(req.Filter, &advFilters); err != nil {
			return req, filter, apperror.NewValidation("invalid filter format").
				WithDetail("error", err.Error())
		}
		if err := domainFilter.ValidateItems(advFilters); err != nil {
			return req, filter, apperror.NewValidation("invalid filter").
				WithDetail("error", err.Error())
		}
		filter.AdvancedFilters = advFilters
	}

	// Inject RLS DataScope from context
	filter.DataScope = security.GetDataScope(c.Request.Context())

	return req, filter, nil
}

// dtoToMap converts a DTO (any struct/map) to map[string]any via JSON roundtrip.
// This is the simplest approach for extracting field values by key.
func dtoToMap(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal dto: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshal dto: %w", err)
	}
	return m, nil
}

// resolveExportValues post-processes a DTO map to make values human-readable
// for XLSX export. Handles three cases:
//
//  1. Reference fields: column key ending with "Id" (e.g. "counterpartyId") →
//     replaced with the sibling RefDisplay.name (e.g. "counterparty"."name").
//  2. Money fields: MinorUnits stored as int64 → scaled by currency.decimalPlaces.
//  3. Quantity fields: scaled int64 → divided by QuantityScale (10_000).
func resolveExportValues(row map[string]any, columnKeys []string) {
	// ── 1. Resolve reference fields ─────────────────────────────────────
	for _, key := range columnKeys {
		if !strings.HasSuffix(key, "Id") {
			continue
		}
		// Strip "Id" suffix to find the resolved ref object.
		// e.g. "counterpartyId" → "counterparty", "warehouseId" → "warehouse"
		refKey := strings.TrimSuffix(key, "Id")
		refObj, ok := row[refKey].(map[string]any)
		if !ok {
			continue
		}
		if name, ok := refObj["name"].(string); ok && name != "" {
			row[key] = name
		}
	}

	// ── 2. Resolve money fields (MinorUnits → major units) ──────────────
	decimalPlaces := 2.0 // default
	if curr, ok := row["currency"].(map[string]any); ok {
		if dp, ok := curr["decimalPlaces"].(float64); ok {
			decimalPlaces = dp
		}
	}
	divisor := math.Pow(10, decimalPlaces)

	for _, key := range columnKeys {
		if !isMoneyField(key) {
			continue
		}
		if v, ok := row[key].(float64); ok {
			row[key] = v / divisor
		}
	}

	// ── 3. Resolve quantity fields (scaled int → float) ─────────────────
	for _, key := range columnKeys {
		if !isQuantityField(key) {
			continue
		}
		if v, ok := row[key].(float64); ok {
			row[key] = v / float64(quantityScale)
		}
	}
}

// isMoneyField returns true if the column key represents a monetary value.
// Convention: keys containing "amount", "price", or "vat" (case-insensitive).
func isMoneyField(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "amount") ||
		strings.Contains(lower, "price") ||
		strings.Contains(lower, "vat")
}

// isQuantityField returns true if the column key represents a quantity value.
func isQuantityField(key string) bool {
	lower := strings.ToLower(key)
	return strings.Contains(lower, "quantity")
}

// writeExportXLSX builds the XLSX from DTO items and writes it to the response.
func writeExportXLSX(c *gin.Context, title string, req dto.ExportListRequest, dtoItems []any) {
	// Convert columns
	columns := make([]listexport.Column, len(req.Columns))
	columnKeys := make([]string, len(req.Columns))
	for i, col := range req.Columns {
		columns[i] = listexport.Column{Key: col.Key, Label: col.Label}
		columnKeys[i] = col.Key
	}

	// Convert DTOs to maps and resolve display values
	rows := make([]map[string]any, 0, len(dtoItems))
	for _, item := range dtoItems {
		m, err := dtoToMap(item)
		if err != nil {
			continue // skip malformed items
		}
		resolveExportValues(m, columnKeys)
		rows = append(rows, m)
	}

	// Set response headers for XLSX download
	filename := fmt.Sprintf("%s.xlsx", title)
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Status(http.StatusOK)

	if err := listexport.XLSX(c.Writer, title, columns, rows); err != nil {
		// Headers already sent — log but can't change status
		_ = c.Error(err)
	}
}

// ExportTablePart handles POST /export-table-part.
// This is a stateless XLSX renderer: the frontend sends pre-resolved rows
// (human-readable names, already-scaled amounts) and the backend only renders
// the spreadsheet. No entity-specific logic, no DB access.
func ExportTablePart(c *gin.Context) {
	var req dto.ExportTablePartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewValidation("invalid request body").WithDetail("error", err.Error()))
		c.Abort()
		return
	}

	if len(req.Rows) > ExportMaxRows {
		_ = c.Error(apperror.NewValidation(fmt.Sprintf("too many rows: %d (max %d)", len(req.Rows), ExportMaxRows)))
		c.Abort()
		return
	}

	// Convert columns
	columns := make([]listexport.Column, len(req.Columns))
	for i, col := range req.Columns {
		columns[i] = listexport.Column{Key: col.Key, Label: col.Label}
	}

	// Title for the XLSX file: "DocumentTitle — TablePartTitle"
	xlsxTitle := fmt.Sprintf("%s — %s", req.DocumentTitle, req.Title)
	filename := fmt.Sprintf("%s.xlsx", xlsxTitle)

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Status(http.StatusOK)

	if err := listexport.XLSX(c.Writer, xlsxTitle, columns, req.Rows); err != nil {
		_ = c.Error(err)
	}
}

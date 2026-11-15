package handlers

import (
	"bytes"
	"fmt"
	"metapus/internal/core/types"
	"metapus/internal/infrastructure/http/httpvalue"
	"metapus/internal/infrastructure/http/jsoncodec"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"

	"metapus/internal/core/apperror"
	"metapus/internal/core/json"
	"metapus/internal/core/security"
	"metapus/internal/domain"
	domainFilter "metapus/internal/domain/filter"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/listexport"
)

// ExportMaxRows is the safety limit for list export.
// Prevents OOM on large datasets. For larger exports, use background reports.
const ExportMaxRows = 50_000

// ParseExportRequest parses and validates an ExportListRequest from the gin context.
func ParseExportRequest(c *gin.Context) (dto.ExportListRequest, domain.ListFilter, error) {
	var req dto.ExportListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return req, domain.ListFilter{}, apperror.NewInvalidRequest(err)
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
		if err := json.UnmarshalPreserveNumbers(req.Filter, &advFilters); err != nil {
			return req, filter, apperror.NewValidation("invalid filter format").WithCause(err)
		}
		if err := domainFilter.ValidateItems(advFilters); err != nil {
			return req, filter, apperror.NewValidation("invalid filter").WithCause(err)
		}
		filter.AdvancedFilters = advFilters
	}

	// Inject RLS DataScope from context
	filter.DataScope = security.GetDataScope(c.Request.Context())

	return req, filter, nil
}

// DTOToMap converts a DTO (any struct/map) to map[string]any via JSON roundtrip.
// This is the simplest approach for extracting field values by key.
func DTOToMap(v any) (map[string]any, error) {
	data, err := jsoncodec.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal dto: %w", err)
	}
	var m map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("unmarshal dto: %w", err)
	}
	markMoneyFields(reflect.ValueOf(v), m)
	return m, nil
}

// markMoneyFields retains declared monetary types after the independent JSON
// projection. Column spelling is not evidence that a field contains money.
func markMoneyFields(value reflect.Value, row map[string]any) {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Map && value.Type().Key().Kind() == reflect.String {
		iter := value.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			if _, present := row[key]; !present {
				continue
			}
			if amount, ok := iter.Value().Interface().(interface{ MinorUnitsValue() int64 }); ok {
				row[key] = types.MinorUnits(amount.MinorUnitsValue())
			}
		}
	}
	if value.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		key := strings.Split(field.Tag.Get("json"), ",")[0]
		if key == "-" {
			continue
		}
		if field.Anonymous && key == "" {
			markMoneyFields(value.Field(i), row)
			continue
		}
		if key == "" {
			key = field.Name
		}
		if _, present := row[key]; !present {
			continue
		}
		if amount, ok := value.Field(i).Interface().(interface{ MinorUnitsValue() int64 }); ok {
			row[key] = types.MinorUnits(amount.MinorUnitsValue())
		}
	}
}

func resolveExportValues(row map[string]any, columnKeys []string) error {
	for _, key := range columnKeys {
		if strings.HasSuffix(key, "Id") {
			ref, ok := row[strings.TrimSuffix(key, "Id")].(map[string]any)
			if ok {
				if name, ok := ref["name"].(string); ok {
					row[key] = name
				}
			}
		}
		amount, monetary := row[key].(types.MinorUnits)
		if !monetary {
			continue
		}
		currency, ok := row["currency"].(map[string]any)
		if !ok {
			return fmt.Errorf("currency metadata is required to export %s", key)
		}
		scale, ok := currency["decimalPlaces"].(json.Number)
		if !ok {
			return fmt.Errorf("currency scale is required to export %s", key)
		}
		dp, err := scale.Int64()
		if err != nil || dp < 0 || dp > 18 {
			return fmt.Errorf("invalid currency scale")
		}
		row[key] = listexport.MoneyCell{MinorUnits: int64(amount), DecimalPlaces: int(dp)}
	}
	// Quantity's JSON codec already outputs major units. Do not scale it again.
	return nil
}

// WriteExportXLSX builds the XLSX from DTO items and writes it to the response.
func WriteExportXLSX(c *gin.Context, title string, req dto.ExportListRequest, dtoItems []any) {
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
		m, err := DTOToMap(item)
		if err != nil {
			_ = c.Error(apperror.NewInternal(err))
			c.Abort()
			return
		}
		if err := resolveExportValues(m, columnKeys); err != nil {
			_ = c.Error(apperror.NewValidation("cannot format export").WithCause(err))
			c.Abort()
			return
		}
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
// (human-readable names, minor-unit strings with currency scale) and the backend only renders
// the spreadsheet. No entity-specific logic, no DB access.
func ExportTablePart(c *gin.Context) {
	var req dto.ExportTablePartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(apperror.NewInvalidRequest(err))
		c.Abort()
		return
	}

	if len(req.Rows) > ExportMaxRows {
		_ = c.Error(apperror.NewValidation(fmt.Sprintf("too many rows: %d (max %d)", len(req.Rows), ExportMaxRows)))
		c.Abort()
		return
	}

	for _, column := range req.Columns {
		if column.Type != "money" {
			continue
		}
		if column.DecimalPlaces == nil {
			_ = c.Error(apperror.NewValidation("currency scale is required"))
			c.Abort()
			return
		}
		for _, row := range req.Rows {
			raw, ok := row[column.Key].(string)
			if !ok {
				_ = c.Error(apperror.NewValidation("export amount must be a minor-unit string"))
				c.Abort()
				return
			}
			amount, err := httpvalue.ParseMinorUnits(raw)
			if err != nil {
				_ = c.Error(apperror.NewValidation("invalid export amount").WithCause(err))
				c.Abort()
				return
			}
			row[column.Key] = listexport.MoneyCell{MinorUnits: amount.MinorUnitsValue(), DecimalPlaces: *column.DecimalPlaces}
		}
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

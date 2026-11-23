package automationreport

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/application/automation"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/automations"
	reportcontract "github.com/alex-bogatiuk/metapus/internal/domain/reports/contract"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/schema"
	"github.com/alex-bogatiuk/metapus/internal/domain/settings"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/compiler"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/reporting/export"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

// _fileExpirationTTL is how long generated report files are kept before cleanup.
const _fileExpirationTTL = 24 * time.Hour

// VariantLoader loads a saved report variant by ID.
// Defined as interface to avoid importing the variant repository directly.
type VariantLoader interface {
	GetVariantConfig(ctx context.Context, variantID string) (*VariantSnapshot, error)
}

// VariantSnapshot is the minimal set of data needed from a saved variant.
type VariantSnapshot struct {
	Name           string
	SelectedFields []string
	VisibleColumns []string
	GroupBy        []string
	SortColumn     *string
	SortDirection  string
	Filters        map[string]any
}

// SettingsLoader loads tenant-wide settings for timezone resolution.
type SettingsLoader interface {
	GetSettings(ctx context.Context) (*settings.Settings, error)
}

// ReportGenerator orchestrates report generation for the Automation Engine.
// It reuses the existing Query Engine (Compiler) and XLSX export pipeline.
type ReportGenerator struct {
	principals interface {
		ContextFor(context.Context, id.ID) (context.Context, error)
	}
	compiler       *compiler.Compiler
	variantLoader  VariantLoader
	fileRepo       automations.FileRepository
	settingsLoader SettingsLoader
}

// NewReportGenerator creates a new report generator.
func NewReportGenerator(
	comp *compiler.Compiler,
	vl VariantLoader,
	fr automations.FileRepository,
	sl SettingsLoader,
	principals interface {
		ContextFor(context.Context, id.ID) (context.Context, error)
	},
) *ReportGenerator {
	return &ReportGenerator{
		principals:     principals,
		compiler:       comp,
		variantLoader:  vl,
		fileRepo:       fr,
		settingsLoader: sl,
	}
}

// GenerateAndStore executes a report, exports it to XLSX, and stores the file
// in sys_automation_files. Returns a lightweight reference for delivery.
//
// Returns (nil, nil) if the report is empty and IncludeEmptyReport is false.
func (g *ReportGenerator) GenerateAndStore(
	ctx context.Context,
	rule *automations.Rule,
	triggerTime time.Time,
) (*automation.GeneratedReportRef, error) {
	config := rule.ReportConfig
	if config == nil {
		return nil, fmt.Errorf("rule %s has no report_config", rule.ID)
	}
	if g.principals == nil {
		return nil, fmt.Errorf("report executor loader is unavailable")
	}
	var err error
	ctx, err = g.principals.ContextFor(ctx, config.ExecutorUserID)
	if err != nil {
		return nil, err
	}

	// 1. Resolve dataset
	ds := g.compiler.GetDataset(config.DatasetKey)
	if ds == nil {
		return nil, fmt.Errorf("unknown dataset: %q", config.DatasetKey)
	}

	// 2. Resolve timezone: per-rule override → tenant settings → UTC
	loc := g.resolveTimezone(ctx, config.Timezone)

	// 3. Resolve period
	period := automation.ResolvePeriod(config.PeriodType, triggerTime, loc, config.CustomDays)
	if config.PeriodType == automations.PeriodAsOfNow {
		period = automation.ResolvedPeriod{From: triggerTime.In(loc), To: triggerTime.In(loc)}
	}

	// 4. Build QueryRequest from variant + period + extra filters
	req, variantName, err := g.buildQueryRequest(ctx, config, period)
	if err != nil {
		return nil, fmt.Errorf("build query request: %w", err)
	}

	// 5. Execute query via Compiler
	result, err := g.compiler.Execute(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("execute report %q: %w", config.DatasetKey, err)
	}

	// 6. Empty check
	if len(result.Items) == 0 && !config.IncludeEmptyReport {
		return nil, nil // skip delivery
	}

	// 7. Build ReportMeta for XLSX export
	meta := g.buildReportMeta(ds, req)

	// 8. Export to XLSX
	var buf bytes.Buffer
	if err := export.XLSX(&buf, meta, result.Items, req.ExportColumns, req.ExportGroupBy); err != nil {
		return nil, fmt.Errorf("xlsx export: %w", err)
	}

	// 9. Save to sys_automation_files
	fileName := fmt.Sprintf("%s_%s.xlsx", ds.Key, period.To.Format("2006-01-02"))
	fileMeta := map[string]any{
		"datasetKey": config.DatasetKey,
		"periodFrom": period.From.Format("2006-01-02"),
		"periodTo":   period.To.Format("2006-01-02"),
	}
	if variantName != "" {
		fileMeta["variantName"] = variantName
	}

	file := &automations.AutomationFile{
		RuleID:    rule.ID,
		FileName:  fileName,
		MimeType:  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		FileData:  buf.Bytes(),
		FileSize:  buf.Len(),
		RowCount:  len(result.Items),
		Metadata:  fileMeta,
		ExpiresAt: time.Now().Add(_fileExpirationTTL),
	}
	if err := g.fileRepo.Save(ctx, file); err != nil {
		return nil, fmt.Errorf("save report file: %w", err)
	}

	return &automation.GeneratedReportRef{
		FileID:      file.ID.String(),
		FileName:    file.FileName,
		RowCount:    len(result.Items),
		Period:      period,
		DatasetName: ds.Name,
	}, nil
}

// LoadAttachment retrieves a generated file through the application port.
func (g *ReportGenerator) LoadAttachment(ctx context.Context, fileID string) (automation.Attachment, error) {
	parsed, err := id.Parse(fileID)
	if err != nil {
		return automation.Attachment{}, fmt.Errorf("parse generated report file id: %w", err)
	}
	file, err := g.fileRepo.GetByID(ctx, parsed)
	if err != nil {
		return automation.Attachment{}, fmt.Errorf("load generated report file: %w", err)
	}
	return automation.Attachment{FileName: file.FileName, MimeType: file.MimeType, Data: file.FileData}, nil
}

// resolveTimezone determines the timezone for period calculation.
// Priority: per-rule override → tenant settings → UTC.
func (g *ReportGenerator) resolveTimezone(ctx context.Context, override *string) *time.Location {
	// Per-rule override
	if override != nil && *override != "" {
		if loc, err := time.LoadLocation(*override); err == nil {
			return loc
		}
		logger.Warn(ctx, "invalid timezone in report config, falling back to tenant settings", "timezone", *override)
	}

	// Tenant settings
	if g.settingsLoader != nil {
		if s, err := g.settingsLoader.GetSettings(ctx); err == nil && s.General.Timezone != "" {
			if loc, err := time.LoadLocation(s.General.Timezone); err == nil {
				return loc
			}
		}
	}

	return time.UTC
}

// buildQueryRequest constructs a QueryRequest from report config + variant + period.
// Returns the request, the variant name (for metadata), and any error.
func (g *ReportGenerator) buildQueryRequest(
	ctx context.Context,
	config *automations.ReportActionConfig,
	period automation.ResolvedPeriod,
) (compiler.QueryRequest, string, error) {
	req := compiler.QueryRequest{
		Dataset: config.DatasetKey,
	}
	var variantName string

	// Load variant if specified
	if config.VariantID != nil && g.variantLoader != nil {
		variant, err := g.variantLoader.GetVariantConfig(ctx, config.VariantID.String())
		if err != nil {
			return req, "", fmt.Errorf("load variant: %w", err)
		}
		if variant != nil {
			variantName = variant.Name
			req.Select = variant.SelectedFields
			req.GroupBy = variant.GroupBy
			req.ExportColumns = variant.VisibleColumns
			req.ExportGroupBy = variant.GroupBy
			if variant.SortColumn != nil {
				req.OrderBy = *variant.SortColumn
				req.OrderDir = variant.SortDirection
			}
			// Start with variant's filters
			req.Filters = maps.Clone(variant.Filters)
		}
	}

	// Ensure filters map exists
	if req.Filters == nil {
		req.Filters = make(map[string]any)
	}

	// Bind only declared dataset parameters; period_from/period_to were never
	// consumed by these executors and silently produced unbounded reports.
	dataset := g.compiler.GetDataset(config.DatasetKey)
	until := period.To.AddDate(0, 0, 1)
	if config.PeriodType == automations.PeriodAsOfNow {
		until = period.To.Add(time.Nanosecond)
	}
	for _, parameter := range dataset.Filters {
		switch parameter.Key {
		case "as_of_date":
			req.Filters[parameter.Key] = until.Add(-time.Nanosecond).Format(time.RFC3339Nano)
		case "from_date":
			req.Filters[parameter.Key] = period.From.Format(time.RFC3339)
		case "to_date":
			req.Filters[parameter.Key] = until.Format(time.RFC3339Nano)
		}
	}

	// Merge extra filters (override variant filters)
	maps.Copy(req.Filters, config.ExtraFilters)

	return req, variantName, nil
}

// buildReportMeta constructs the ReportMeta needed by export.XLSX.
func (g *ReportGenerator) buildReportMeta(ds *schema.Dataset, req compiler.QueryRequest) reportcontract.ReportMeta {
	return g.compiler.DatasetMeta(ds.Key)
}

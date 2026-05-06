package automations

import (
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

// PeriodType defines how the report period is calculated relative to trigger time.
type PeriodType string

const (
	// PeriodToday generates the report for today: [00:00, 23:59].
	PeriodToday PeriodType = "today"
	// PeriodYesterday generates the report for yesterday.
	PeriodYesterday PeriodType = "yesterday"
	// PeriodCurrentWeek generates the report from Monday of the current week to today.
	PeriodCurrentWeek PeriodType = "current_week"
	// PeriodLastWeek generates the report for the full previous week (Mon–Sun).
	PeriodLastWeek PeriodType = "last_week"
	// PeriodCurrentMonth generates the report from the 1st of the current month to today.
	PeriodCurrentMonth PeriodType = "current_month"
	// PeriodLastMonth generates the report for the full previous month.
	PeriodLastMonth PeriodType = "last_month"
	// PeriodAsOfNow generates a point-in-time report (e.g. stock balance as of now).
	PeriodAsOfNow PeriodType = "as_of_now"
	// PeriodCustomDays generates the report for the last N days.
	PeriodCustomDays PeriodType = "custom_days"
)

// _validPeriodTypes is a lookup set for validation.
var _validPeriodTypes = map[PeriodType]bool{
	PeriodToday: true, PeriodYesterday: true,
	PeriodCurrentWeek: true, PeriodLastWeek: true,
	PeriodCurrentMonth: true, PeriodLastMonth: true,
	PeriodAsOfNow: true, PeriodCustomDays: true,
}

// ReportActionConfig stores the configuration for a "generate_report" reaction.
// Persisted as JSONB in sys_automation_rules.report_config.
type ReportActionConfig struct {
	// DatasetKey is the report dataset key, e.g. "stock-balance".
	DatasetKey string `json:"datasetKey"`

	// VariantID is the optional saved report variant ID.
	// If set, variant's SelectedFields, Filters, GroupBy, SortColumn are used.
	// If nil, dataset defaults are used.
	VariantID *id.ID `json:"variantId,omitempty"`

	// PeriodType defines how to calculate the date range.
	PeriodType PeriodType `json:"periodType"`

	// CustomDays is used when PeriodType == "custom_days". Last N days.
	CustomDays int `json:"customDays,omitempty"`

	// ExtraFilters are additional filters merged ON TOP of variant filters.
	// E.g. restrict to specific warehouse even if variant doesn't filter it.
	ExtraFilters map[string]any `json:"extraFilters,omitempty"`

	// FileFormat is the export format: "xlsx" (default) or "csv".
	FileFormat string `json:"fileFormat,omitempty"`

	// IncludeEmptyReport controls whether to send the report if it has 0 rows.
	IncludeEmptyReport bool `json:"includeEmptyReport,omitempty"`

	// Timezone is a per-rule IANA timezone override.
	// If nil, the tenant-wide timezone from sys_settings.general is used.
	Timezone *string `json:"timezone,omitempty"`
}

// Validate checks if the ReportActionConfig is valid.
// Pure function — no DB access.
func (c *ReportActionConfig) Validate() error {
	if c.DatasetKey == "" {
		return apperror.NewValidation("report dataset key is required").
			WithDetail("field", "reportConfig.datasetKey")
	}

	if !_validPeriodTypes[c.PeriodType] {
		return apperror.NewValidation("invalid period type: " + string(c.PeriodType)).
			WithDetail("field", "reportConfig.periodType")
	}

	if c.PeriodType == PeriodCustomDays && c.CustomDays < 1 {
		return apperror.NewValidation("custom_days period requires customDays >= 1").
			WithDetail("field", "reportConfig.customDays")
	}

	if c.FileFormat != "" && c.FileFormat != "xlsx" && c.FileFormat != "csv" {
		return apperror.NewValidation("unsupported file format: " + c.FileFormat).
			WithDetail("field", "reportConfig.fileFormat")
	}

	return nil
}

// GetFileFormat returns the effective file format, defaulting to "xlsx".
func (c *ReportActionConfig) GetFileFormat() string {
	if c.FileFormat != "" {
		return c.FileFormat
	}
	return "xlsx"
}

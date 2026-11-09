// Package settings provides the domain model for tenant-level system settings.
// Settings are stored as a single row in sys_settings.
// Organization-specific settings (requisites, accounting policy) live in cat_organizations.
package settings

import (
	"metapus/internal/core/id"
	"time"
)

// Settings represents the tenant-wide system configuration.
// Only system-level settings remain here; org-specific data is in cat_organizations.
type Settings struct {
	// General
	General     GeneralSettings     `json:"general"`
	Numbering   NumberingSettings   `json:"numbering"`
	Performance PerformanceSettings `json:"performance"`

	// Module-scoped
	ExchangeRates ExchangeRatesSettings `json:"exchangeRates"`

	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ── General ──────────────────────────────────────────────────────────────

// GeneralSettings holds tenant-wide general configuration.
type GeneralSettings struct {
	// Timezone is an IANA timezone identifier, e.g. "Asia/Shanghai", "Europe/Moscow".
	// Used as the default timezone for scheduled operations (report distribution, etc.).
	Timezone string `json:"timezone"`
}

// DefaultGeneral returns sensible defaults for general settings.
func DefaultGeneral() GeneralSettings {
	return GeneralSettings{
		Timezone: "UTC",
	}
}

// ── Numbering ───────────────────────────────────────────────────────────

// NumberingSettings holds document auto-numbering parameters (system-wide).
type NumberingSettings struct {
	AutoNumbering bool   `json:"autoNumbering"`
	NumberPrefix  string `json:"numberPrefix"`
}

// DefaultNumbering returns sensible defaults for numbering settings.
func DefaultNumbering() NumberingSettings {
	return NumberingSettings{
		AutoNumbering: true,
		NumberPrefix:  "",
	}
}

// ── Performance ─────────────────────────────────────────────────────────

// PerformanceSettings holds processing and parallelism parameters.
type PerformanceSettings struct {
	// BatchConcurrency controls how many documents are processed in parallel
	// during batch operations (post, unpost, deletion mark).
	// Valid range: 1 .. MaxConnsPerTenant/2. Default: 5.
	BatchConcurrency int `json:"batchConcurrency"`
}

// DefaultPerformance returns sensible defaults for performance settings.
func DefaultPerformance() PerformanceSettings {
	return PerformanceSettings{
		BatchConcurrency: 5,
	}
}

// ClampBatchConcurrency ensures the value is within [1, maxPoolHalf].
func ClampBatchConcurrency(value, maxConnsPerTenant int) int {
	maxPoolHalf := max(maxConnsPerTenant/2, 1)
	if value < 1 {
		return 1
	}
	if value > maxPoolHalf {
		return maxPoolHalf
	}
	return value
}

// ExchangeRatesSettings selects one provider; USD and freshness are fixed in v1.
type ExchangeRatesSettings struct {
	SourceID *id.ID `json:"sourceId"`
}

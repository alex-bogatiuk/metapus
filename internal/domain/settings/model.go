// Package settings provides the domain model for tenant-level system settings.
// Settings are stored as a single row in sys_settings (analogous to 1C "Constants").
package settings

import "time"

// Settings represents the full tenant configuration.
type Settings struct {
	Organization OrganizationSettings `json:"organization"`
	Accounting   AccountingSettings   `json:"accounting"`
	Performance  PerformanceSettings  `json:"performance"`
	Version      int                  `json:"version"`
	UpdatedAt    time.Time            `json:"updatedAt"`
}

// ── Organization ────────────────────────────────────────────────────────

// OrganizationSettings holds company identification and contacts.
type OrganizationSettings struct {
	CompanyName   string `json:"companyName"`
	ShortName     string `json:"shortName"`
	INN           string `json:"inn"`
	KPP           string `json:"kpp"`
	OGRN          string `json:"ogrn"`
	LegalAddress  string `json:"legalAddress"`
	ActualAddress string `json:"actualAddress"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Website       string `json:"website"`
	Director      string `json:"director"`
	Accountant    string `json:"accountant"`
	LogoURL       string `json:"logoUrl"`
}

// ── Accounting ──────────────────────────────────────────────────────────

// AccountingSettings holds tax, VAT, inventory, and numbering parameters.
type AccountingSettings struct {
	DefaultCurrency string `json:"defaultCurrency"`
	TaxSystem       string `json:"taxSystem"`
	VatPayer        bool   `json:"vatPayer"`
	DefaultVatRate  string `json:"defaultVatRate"`
	InventoryMethod string `json:"inventoryMethod"`
	FiscalYearStart string `json:"fiscalYearStart"`
	AutoNumbering   bool   `json:"autoNumbering"`
	NumberPrefix    string `json:"numberPrefix"`
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
	maxPoolHalf := maxConnsPerTenant / 2
	if maxPoolHalf < 1 {
		maxPoolHalf = 1
	}
	if value < 1 {
		return 1
	}
	if value > maxPoolHalf {
		return maxPoolHalf
	}
	return value
}

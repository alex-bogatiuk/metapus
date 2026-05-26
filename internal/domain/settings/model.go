// Package settings provides the domain model for tenant-level system settings.
// Settings are stored as a single row in sys_settings.
// Organization-specific settings (requisites, accounting policy) live in cat_organizations.
package settings

import "time"

// Settings represents the tenant-wide system configuration.
// Only system-level settings remain here; org-specific data is in cat_organizations.
type Settings struct {
	// General
	General     GeneralSettings     `json:"general"`
	Numbering   NumberingSettings   `json:"numbering"`
	Performance PerformanceSettings `json:"performance"`

	// Module-scoped
	Warehouse  WarehouseSettings  `json:"warehouse"`
	Sales      SalesSettings      `json:"sales"`
	Purchasing PurchasingSettings `json:"purchasing"`

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

// ── Warehouse ───────────────────────────────────────────────────────────

// WarehouseSettings holds inventory and stock management parameters.
type WarehouseSettings struct {
	// InventoryMethod defines the costing method: "fifo" or "weighted_average".
	InventoryMethod string `json:"inventoryMethod"`
	// NegativeStockControl prevents posting when stock would go below zero.
	NegativeStockControl bool `json:"negativeStockControl"`
	// AutoPostReceipts automatically posts goods receipts upon saving.
	AutoPostReceipts bool `json:"autoPostReceipts"`
}

// DefaultWarehouse returns sensible defaults for warehouse settings.
func DefaultWarehouse() WarehouseSettings {
	return WarehouseSettings{
		InventoryMethod:      "fifo",
		NegativeStockControl: true,
		AutoPostReceipts:     false,
	}
}

// ── Sales ────────────────────────────────────────────────────────────────

// SalesSettings holds sales module parameters.
type SalesSettings struct {
	// DefaultPaymentTermDays is the default payment deadline in days for new invoices.
	DefaultPaymentTermDays int `json:"defaultPaymentTermDays"`
	// AutoReserveStock automatically reserves stock when a sales order is confirmed.
	AutoReserveStock bool `json:"autoReserveStock"`
}

// DefaultSales returns sensible defaults for sales settings.
func DefaultSales() SalesSettings {
	return SalesSettings{
		DefaultPaymentTermDays: 30,
		AutoReserveStock:       false,
	}
}

// ── Purchasing ──────────────────────────────────────────────────────────

// PurchasingSettings holds purchasing module parameters.
type PurchasingSettings struct {
	// DefaultPaymentTermDays is the default payment deadline in days for purchase orders.
	DefaultPaymentTermDays int `json:"defaultPaymentTermDays"`
	// RequireApproval requires manager approval for purchase orders above a threshold.
	RequireApproval bool `json:"requireApproval"`
}

// DefaultPurchasing returns sensible defaults for purchasing settings.
func DefaultPurchasing() PurchasingSettings {
	return PurchasingSettings{
		DefaultPaymentTermDays: 30,
		RequireApproval:        false,
	}
}

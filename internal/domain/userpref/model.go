// Package userpref provides user preferences domain logic.
// User preferences are per-user UI settings (theme, list filters, columns)
// stored separately from tenant-level settings.
package userpref

import (
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

// InterfacePrefs holds typed UI settings for a user.
// All fields are optional (omitempty) — partial updates are supported.
type InterfacePrefs struct {
	Theme            string `json:"theme,omitempty"` // "light"|"dark"|"system"
	Language         string `json:"language,omitempty"`
	DateFormat       string `json:"dateFormat,omitempty"`
	NumberFormat     string `json:"numberFormat,omitempty"`
	ShowTooltips     *bool  `json:"showTooltips,omitempty"`
	CompactMode      *bool  `json:"compactMode,omitempty"`
	SidebarCollapsed *bool  `json:"sidebarCollapsed,omitempty"`
	// Table parts work like 1C: a click selects a cell, a double click,
	// Enter or F2 edits it, and the keyboard moves and selects lines.
	TablePartCellMode *bool `json:"tablePartCellMode,omitempty"`
	// Per-entity toggle: show deletion-marked items in list views. Key = entity type (e.g. "Counterparty").
	ShowDeletedEntities map[string]bool `json:"showDeletedEntities,omitempty"`
	// Per-section toggle: whether a collapsible UI section is expanded. Key = section id owned by the UI.
	ExpandedSections map[string]bool `json:"expandedSections,omitempty"`
}

// UserPreferences represents all preferences for a single user (1 row in DB).
type UserPreferences struct {
	UserID          id.ID           `db:"user_id"          json:"userId"`
	Interface       InterfacePrefs  `db:"interface"         json:"interface"`
	ListFilters     json.RawMessage `db:"list_filters"      json:"listFilters"`     // opaque JSON, frontend owns schema
	ListColumns     json.RawMessage `db:"list_columns"      json:"listColumns"`     // opaque JSON
	DashboardLayout json.RawMessage `db:"dashboard_layout"  json:"dashboardLayout"` // opaque JSON, frontend owns schema
	Favorites       json.RawMessage `db:"favorites"         json:"favorites"`       // opaque JSON array, frontend owns schema
	UpdatedAt       time.Time       `db:"updated_at"        json:"updatedAt"`
}

// NewDefault creates empty preferences for a user.
func NewDefault(userID id.ID) *UserPreferences {
	return &UserPreferences{
		UserID:          userID,
		Interface:       InterfacePrefs{},
		ListFilters:     json.RawMessage("{}"),
		ListColumns:     json.RawMessage("{}"),
		DashboardLayout: json.RawMessage("null"),
		Favorites:       json.RawMessage("[]"),
	}
}

// Package userpref provides user preferences domain logic.
// User preferences are per-user UI settings (theme, list filters, columns)
// stored separately from tenant-level settings.
package userpref

import (
	"encoding/json"
	"time"

	"metapus/internal/core/id"
)

// InterfacePrefs holds typed UI settings for a user.
// All fields are optional (omitempty) — partial updates are supported.
type InterfacePrefs struct {
	Theme            string `json:"theme,omitempty"` // "light"|"dark"|"system"
	Language         string `json:"language,omitempty"`
	DateFormat       string `json:"dateFormat,omitempty"`
	NumberFormat     string `json:"numberFormat,omitempty"`
	PageSize         int    `json:"pageSize,omitempty"`
	ShowTooltips     *bool  `json:"showTooltips,omitempty"`
	CompactMode      *bool  `json:"compactMode,omitempty"`
	SidebarCollapsed *bool  `json:"sidebarCollapsed,omitempty"`
	// Per-entity toggle: show deletion-marked items in list views. Key = entity type (e.g. "GoodsReceipt").
	ShowDeletedEntities map[string]bool `json:"showDeletedEntities,omitempty"`
}

// UserPreferences represents all preferences for a single user (1 row in DB).
type UserPreferences struct {
	UserID      id.ID           `db:"user_id"      json:"userId"`
	Interface   InterfacePrefs  `db:"interface"     json:"interface"`
	ListFilters json.RawMessage `db:"list_filters"  json:"listFilters"` // opaque JSON, frontend owns schema
	ListColumns json.RawMessage `db:"list_columns"  json:"listColumns"` // opaque JSON
	UpdatedAt   time.Time       `db:"updated_at"    json:"updatedAt"`
}

// NewDefault creates empty preferences for a user.
func NewDefault(userID id.ID) *UserPreferences {
	return &UserPreferences{
		UserID:      userID,
		Interface:   InterfacePrefs{},
		ListFilters: json.RawMessage("{}"),
		ListColumns: json.RawMessage("{}"),
	}
}

package dto

import (
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain/notifications"
	"github.com/alex-bogatiuk/metapus/internal/domain/reports/tabular"
)

type UpdateSettingsSectionRequest struct {
	Data    json.RawMessage `json:"data" binding:"required"`
	Version int             `json:"version" binding:"required"`
}

// The route fixes the section type; JSONB persistence remains independent of HTTP.
type SettingsSectionRequest[T any] struct {
	Data    T   `json:"data" binding:"required"`
	Version int `json:"version" binding:"required"`
}

type SystemVersionResponse struct {
	Version               string `json:"version"`
	BuildTime             string `json:"buildTime"`
	ExpectedSchemaVersion int    `json:"expectedSchemaVersion"`
}

type PoolStatsResponse struct {
	TotalConns    int32 `json:"total_conns"`
	AcquiredConns int32 `json:"acquired_conns"`
	IdleConns     int32 `json:"idle_conns"`
	MaxConns      int32 `json:"max_conns,omitzero"`
}

type TenantPoolStatsResponse struct {
	ActivePools   int `json:"active_pools"`
	TotalConns    int `json:"total_conns"`
	IdleConns     int `json:"idle_conns"`
	AcquiredConns int `json:"acquired_conn"`
}

type HealthInfoResponse struct {
	App          string                   `json:"app"`
	Version      string                   `json:"version"`
	Mode         string                   `json:"mode,omitempty"`
	Database     *PoolStatsResponse       `json:"database,omitempty"`
	MetaDatabase *PoolStatsResponse       `json:"meta_database,omitempty"`
	Tenants      *TenantPoolStatsResponse `json:"tenants,omitempty"`
}

type EventLogTraceResponse struct {
	Items []EventLogResponse `json:"items"`
}

type NotificationListResponse struct {
	Items       []*notifications.Notification `json:"items"`
	UnreadCount int                           `json:"unreadCount"`
}

type GroupedReportResponse struct {
	Rows       []tabular.DisplayRow `json:"rows"`
	TotalItems int                  `json:"totalItems"`
}

type TenantSummary struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	DisplayName    string `json:"displayName"`
	DBName         string `json:"dbName"`
	Status         string `json:"status"`
	Plan           string `json:"plan"`
	SchemaVersion  int    `json:"schemaVersion"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	SchemaUpToDate bool   `json:"schemaUpToDate"`
}

type TenantListResponse struct {
	Items          []TenantSummary `json:"items"`
	Total          int             `json:"total"`
	ActiveCount    int             `json:"activeCount"`
	OutdatedCount  int             `json:"outdatedCount"`
	ExpectedSchema int             `json:"expectedSchema"`
}

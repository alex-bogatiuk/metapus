package contracts

import (
	"github.com/alex-bogatiuk/metapus/internal/domain/settings"
	"github.com/alex-bogatiuk/metapus/internal/domain/userpref"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
)

func systemContracts() []Contract {
	return []Contract{
		Response[dto.HealthInfoResponse]("HealthInfoResponse"),
		Response[dto.PoolStatsResponse]("PoolStatsResponse"),
		Response[dto.TenantPoolStatsResponse]("TenantPoolStatsResponse"),
		Response[[]dto.PolicyRuleResponse]("PolicyRuleListResponse"),
		Response[settings.Settings]("SystemSettingsResponse"),
		Request[dto.UpdateSettingsSectionRequest]("UpdateSettingsSectionRequest"),
		Request[dto.SettingsSectionRequest[settings.GeneralSettings]]("UpdateGeneralSettingsRequest"),
		Request[dto.SettingsSectionRequest[settings.PerformanceSettings]]("UpdatePerformanceSettingsRequest"),
		Request[dto.SettingsSectionRequest[settings.ExchangeRatesSettings]]("UpdateExchangeRatesSettingsRequest"),
		Response[userpref.UserPreferences]("UserPreferencesResponse"),
		Request[userpref.InterfacePrefs]("UpdateInterfacePreferencesRequest"),
		Response[dto.SystemVersionResponse]("SystemVersionResponse"),
		Response[dto.TypedCursorListResponse[dto.EventLogResponse]]("EventLogCursorResponse"),
		Response[dto.EventLogTraceResponse]("EventLogTraceResponse"),
		Response[dto.NotificationListResponse]("NotificationListResponse"),
		Response[dto.GroupedReportResponse]("GroupedReportResponse"),
		Response[dto.TenantSummary]("TenantSummary"),
		Response[dto.TenantListResponse]("TenantListResponse"),
		Response[[]dto.ListViewResponse]("ListViewListResponse"),
		Response[[]dto.VariantResponse]("VariantListResponse"),
	}
}

func systemRoutes() []RouteContract {
	routes := []RouteContract{}
	add := func(method, path, request, response string) {
		routes = append(routes, RouteContract{Scope: "api", Method: method, Path: path, Request: request, Response: response})
	}
	add("GET", "/settings", "", "SystemSettingsResponse")
	for _, section := range []struct{ path, name string }{
		{"general", "General"}, {"performance", "Performance"}, {"exchangeRates", "ExchangeRates"},
	} {
		add("PATCH", "/settings/"+section.path, "Update"+section.name+"SettingsRequest", "SystemSettingsResponse")
	}
	add("GET", "/system/version", "", "SystemVersionResponse")
	add("GET", "/system/event-log", "", "EventLogCursorResponse")
	add("GET", "/system/event-log/stats", "", "EventLogStatsResponse")
	add("GET", "/system/event-log/trace/:traceId", "", "EventLogTraceResponse")
	add("GET", "/system/event-log/:id", "", "EventLogResponse")
	add("GET", "/system/worker-jobs", "", "WorkerJobListResponse")
	add("GET", "/system/worker-jobs/stats", "", "WorkerJobStatsResponse")
	add("GET", "/system/notifications", "", "NotificationListResponse")
	add("PUT", "/system/notifications/mark-all-read", "", "")
	add("PUT", "/system/notifications/:id/read", "", "")
	add("PUT", "/system/notifications/:id/unread", "", "")
	add("DELETE", "/system/notifications/:id", "", "")
	add("GET", "/admin/tenants", "", "TenantListResponse")
	add("GET", "/admin/tenants/:tenantId", "", "TenantSummary")
	add("GET", "/me/preferences", "", "UserPreferencesResponse")
	add("PUT", "/me/preferences/interface", "UpdateInterfacePreferencesRequest", "")
	add("GET", "/me/list-views/:entityType", "", "ListViewListResponse")
	add("POST", "/me/list-views", "CreateListViewRequest", "ListViewResponse")
	add("PUT", "/me/list-views/:id", "UpdateListViewRequest", "ListViewResponse")
	add("DELETE", "/me/list-views/:id", "", "")
	add("PUT", "/me/list-views/:id/default", "", "")
	add("GET", "/reports/:dataset/metadata", "", "ReportMetadataResponse")
	add("POST", "/reports/:dataset", "DatasetQueryRequest", "DatasetQueryResponse")
	add("POST", "/reports/:dataset/grouped", "DatasetQueryRequest", "GroupedReportResponse")
	add("GET", "/reports/:dataset/variants", "", "VariantListResponse")
	add("POST", "/reports/variants", "CreateVariantRequest", "VariantResponse")
	add("PUT", "/reports/variants/:id", "UpdateVariantRequest", "StatusResponse")
	add("DELETE", "/reports/variants/:id", "", "StatusResponse")
	return routes
}

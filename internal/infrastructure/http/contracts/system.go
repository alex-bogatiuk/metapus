package contracts

import (
	"metapus/internal/domain/settings"
	"metapus/internal/domain/userpref"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/v1/dto"
)

func systemContracts() []Contract {
	return []Contract{
		Response[dto.HealthInfoResponse]("HealthInfoResponse"),
		Response[dto.PoolStatsResponse]("PoolStatsResponse"),
		Response[dto.TenantPoolStatsResponse]("TenantPoolStatsResponse"),
		Response[[]dto.PolicyRuleResponse]("PolicyRuleListResponse"),
		Response[cryptodto.CryptoNetworkListResponse]("CryptoNetworkListResponse"),
		Response[settings.Settings]("SystemSettingsResponse"),
		Request[dto.UpdateSettingsSectionRequest]("UpdateSettingsSectionRequest"),
		Request[dto.SettingsSectionRequest[settings.GeneralSettings]]("UpdateGeneralSettingsRequest"),
		Request[dto.SettingsSectionRequest[settings.NumberingSettings]]("UpdateNumberingSettingsRequest"),
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
		{"general", "General"}, {"numbering", "Numbering"}, {"performance", "Performance"}, {"exchangeRates", "ExchangeRates"},
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
	add("GET", "/crypto/wallet-pools", "", "WalletPoolListResponse")
	add("PATCH", "/crypto/wallet-pools/:networkId/policy", "UpdateWalletPoolPolicyRequest", "WalletPoolStateResponse")
	add("POST", "/crypto/wallet-pools/:networkId/reconcile", "", "WalletPoolReconcileResponse")
	add("GET", "/crypto/wallet-pools/:networkId/merchant-overrides/:merchantId", "", "MerchantWalletAllocationLimitsResponse")
	add("PUT", "/crypto/wallet-pools/:networkId/merchant-overrides/:merchantId", "UpdateMerchantWalletAllocationLimitsRequest", "MerchantWalletAllocationLimitsResponse")
	add("GET", "/admin/crypto/networks", "", "CryptoNetworkListResponse")
	add("GET", "/admin/crypto/networks/:networkId/status", "", "CryptoNetworkStateResponse")
	add("GET", "/admin/crypto/networks/:networkId/rpc-configuration", "", "CryptoRPCConfigurationResponse")
	add("PATCH", "/admin/crypto/networks/:networkId/rpc-configuration", "UpdateCryptoRPCConfigurationRequest", "CryptoRPCConfigurationResponse")
	add("PATCH", "/admin/crypto/networks/:networkId/policy", "UpdateCryptoNetworkPolicyRequest", "CryptoNetworkStateResponse")
	add("POST", "/admin/crypto/networks/:networkId/watching", "SetCryptoNetworkWatchingRequest", "CryptoNetworkStateResponse")
	add("GET", "/admin/crypto/networks/:networkId/shadow", "", "CryptoShadowControlResponse")
	add("POST", "/admin/crypto/networks/:networkId/shadow", "SetCryptoShadowControlRequest", "CryptoShadowControlResponse")
	return routes
}

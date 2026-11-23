package contracts

import (
	"github.com/alex-bogatiuk/metapus/internal/application/exchangerateapp"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/domain"
	er "github.com/alex-bogatiuk/metapus/internal/domain/registers/exchange_rate"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/http/v1/dto"
	"github.com/alex-bogatiuk/metapus/internal/metadata"
)

func serviceOperationContracts() []Contract {
	return []Contract{
		Response[[]dto.EntitySummaryResponse]("EntitySummaryListResponse"),
		Response[[]metadata.EntityDef]("EntityMetadataListResponse"),
		Response[[]metadata.FilterFieldMeta]("FilterFieldsMetadataResponse"),
		Response[map[string]any]("EntityMockResponse"),
		Response[[]domain.RefResolveResult]("ResolvedRefsResponse"),
		// These preferences are opaque JSON to the server; frontend-owned
		// schemas validate filters, columns, favorites and widget configuration.
		Request[json.RawMessage]("UpdateJSONPreferencesRequest"),
		Request[domain.FindReferencesRequest]("FindReferencesRequest"),
		Response[dto.FindReferencesResponse]("FindReferencesResponse"),
		Response[domain.DeletionPlan]("MarkedObjectsResponse"),
		Request[dto.PlanMarkedRequest]("PlanMarkedRequest"),
		Request[dto.DeleteMarkedRequest]("DeleteMarkedRequest"),
		Response[domain.DeleteMarkedResult]("DeleteMarkedResponse"),
		Response[exchangerateapp.CurrentPage]("CurrentExchangeRatesResponse"),
		Response[exchangerateapp.HistoryPage]("ExchangeRateHistoryResponse"),
		Response[exchangerateapp.MappingPage]("ExchangeRateMappingsResponse"),
		Response[er.Quote]("ExchangeRateQuoteResponse"),
		Response[er.Mapping]("ExchangeRateMappingResponse"),
		Response[er.FeedState]("ExchangeRateFeedStateResponse"),
		Request[dto.UpdateRateMappingRequest]("UpdateRateMappingRequest"),
	}
}

func serviceOperationRoutes() []RouteContract {
	routes := []RouteContract{}
	add := func(method, path, request, response string) {
		routes = append(routes, RouteContract{Scope: "api", Method: method, Path: path, Request: request, Response: response})
	}
	add("GET", "/meta", "", "EntityMetadataListResponse")
	add("GET", "/meta/entities", "", "EntitySummaryListResponse")
	add("GET", "/meta/:name", "", "EntityMetadataResponse")
	add("GET", "/meta/:name/filters", "", "FilterFieldsMetadataResponse")
	add("GET", "/meta/:name/mock", "", "EntityMockResponse")
	add("POST", "/resolve-refs", "ResolveRefsRequest", "ResolvedRefsResponse")
	for _, path := range []string{"/me/preferences/list-filters/:entityType", "/me/preferences/list-columns/:entityType", "/me/preferences/dashboard-layout", "/me/preferences/favorites"} {
		add("PUT", path, "UpdateJSONPreferencesRequest", "")
	}
	add("POST", "/system/find-references", "FindReferencesRequest", "FindReferencesResponse")
	add("GET", "/system/marked-objects", "", "MarkedObjectsResponse")
	add("POST", "/system/marked-objects/plan", "PlanMarkedRequest", "MarkedObjectsResponse")
	add("POST", "/system/marked-objects/delete", "DeleteMarkedRequest", "DeleteMarkedResponse")
	add("GET", "/registers/exchange-rates/current", "", "CurrentExchangeRatesResponse")
	add("GET", "/registers/exchange-rates/history", "", "ExchangeRateHistoryResponse")
	add("GET", "/registers/exchange-rates/:id", "", "ExchangeRateQuoteResponse")
	add("POST", "/reports/:dataset/export", "DatasetQueryRequest", "")
	add("POST", "/export-table-part", "ExportTablePartRequest", "")
	return routes
}

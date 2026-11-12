package contracts

import (
	"bytes"
	"fmt"
	"metapus/internal/infrastructure/http/contracts/spec"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// RouteContract binds a method and path to concrete wire types. Paths are server
// route templates, independent of URL parameters and of authorization policy.
type RouteContract = spec.RouteContract

func Routes() []RouteContract {
	routes := []RouteContract{
		{Scope: "api", Method: "GET", Path: "/system/integrations/providers", Response: "IntegrationProvidersResponse"},
		{Scope: "api", Method: "GET", Path: "/system/integrations/pools/:id/status", Response: "IntegrationPoolStatusResponse"},
		{Scope: "api", Method: "POST", Path: "/system/integrations/pools/:id/command", Request: "IntegrationPoolCommandRequest", Response: "IntegrationPoolStatusResponse"},
		{Scope: "api", Method: "POST", Path: "/system/integrations/pools/:id/test", Response: "IntegrationConnectionTestResponse"},
		{Scope: "api", Method: "GET", Path: "/system/integrations/pools/:id/rpc-key-names", Response: "IntegrationRPCKeyOptionsResponse"},
	}
	add := func(scope, method, path, request, response string) {
		routes = append(routes, RouteContract{Scope: scope, Method: method, Path: path, Request: request, Response: response})
	}
	add("api", "GET", "/crypto/unidentified-receipts", "", "UnidentifiedReceiptListResponse")
	add("api", "GET", "/crypto/unidentified-receipts/:id", "", "CustodyReceiptDetailResponse")
	add("api", "POST", "/crypto/unidentified-receipts/:id/credits", "PrepareMerchantCreditRequest", "MerchantCreditResponse")
	add("api", "POST", "/crypto/merchant-credits/:id/approve", "DecideMerchantCreditRequest", "MerchantCreditResponse")
	add("api", "POST", "/crypto/merchant-credits/:id/reject", "DecideMerchantCreditRequest", "MerchantCreditResponse")
	add("api", "POST", "/crypto/unidentified-receipts/:id/platform-fundings", "PreparePlatformFundingRequest", "PlatformFundingResponse")
	add("api", "POST", "/crypto/platform-fundings/:id/approve", "DecidePlatformFundingRequest", "PlatformFundingResponse")
	add("api", "POST", "/crypto/platform-fundings/:id/reject", "DecidePlatformFundingRequest", "PlatformFundingResponse")
	add("api", "GET", "/admin/dashboard/queues", "", "AdminDashboardQueuesResponse")
	add("api", "GET", "/admin/dashboard/finance", "", "AdminDashboardFinanceResponse")
	add("api", "GET", "/admin/dashboard/analytics", "", "AdminDashboardAnalyticsResponse")
	portal := func(method, path, request, response string) { add("portal", method, path, request, response) }
	portal("GET", "/merchants", "", "PortalMerchantListResponse")
	portal("GET", "/dashboard/summary", "", "PortalSummaryResponse")
	portal("GET", "/dashboard/currencies", "", "PortalCurrencyListResponse")
	portal("GET", "/dashboard/chart", "", "PortalChartResponse")
	portal("GET", "/dashboard/funnel", "", "PortalFunnelResponse")
	portal("GET", "/dashboard/balance", "", "PortalBalanceResponse")
	portal("GET", "/balances", "", "PortalDetailedBalanceResponse")
	portal("GET", "/balances/risk", "", "PortalRiskBalanceResponse")
	portal("GET", "/invoices", "", "PortalInvoiceListResponse")
	portal("POST", "/invoices", "PortalCreateInvoiceRequest", "PortalCreateInvoiceResponse")
	portal("GET", "/invoices/:id", "", "PortalInvoiceDetailResponse")
	portal("GET", "/payment-links", "", "PortalPaymentLinkListResponse")
	portal("POST", "/payment-links", "CreatePaymentLinkRequest", "PortalPaymentLinkCreateResponse")
	portal("GET", "/deposit-addresses", "", "PortalDepositAddressListResponse")
	portal("POST", "/deposit-addresses", "PortalCreateDepositAddressRequest", "PortalCreateDepositAddressResponse")
	portal("POST", "/deposit-addresses/:id/rotate", "PortalRotateDepositAddressRequest", "PortalRotateDepositAddressResponse")
	portal("GET", "/settings", "", "PortalSettingsResponse")
	portal("PATCH", "/settings", "UpdatePortalSettingsRequest", "PortalSettingsResponse")
	portal("GET", "/settings/fees", "", "PortalFeeScheduleResponse")
	portal("GET", "/withdrawals", "", "PortalWithdrawalListResponse")
	portal("GET", "/withdrawal-addresses", "", "PortalWithdrawalAddressListResponse")
	portal("POST", "/withdrawal-addresses", "PortalAddWithdrawalAddressRequest", "PortalWithdrawalAddressCreatedResponse")
	portal("DELETE", "/withdrawal-addresses/:id", "", "")
	portal("GET", "/withdrawal-requests", "", "PortalWithdrawalRequestListResponse")
	portal("POST", "/withdrawal-requests", "PortalCreateWithdrawalRequest", "PortalWithdrawalCreatedResponse")
	portal("POST", "/withdrawal-requests/:id/reject", "ReasonRequest", "StatusResponse")
	portal("GET", "/webhook-endpoints", "", "WebhookEndpointListResponse")
	portal("POST", "/webhook-endpoints", "CreateWebhookEndpointRequest", "WebhookEndpointResponse")
	portal("GET", "/webhook-endpoints/:id", "", "WebhookEndpointResponse")
	portal("PATCH", "/webhook-endpoints/:id", "UpdateWebhookEndpointRequest", "WebhookEndpointResponse")
	portal("DELETE", "/webhook-endpoints/:id", "", "")
	portal("POST", "/webhook-endpoints/:id/rotate-secret", "", "WebhookEndpointResponse")
	portal("POST", "/webhook-endpoints/:id/test", "", "WebhookTestResponse")
	portal("GET", "/events", "", "MerchantEventListResponse")
	portal("GET", "/events/:id/deliveries", "", "MerchantEventDeliveriesResponse")
	portal("POST", "/events/:id/redeliver", "RedeliverMerchantEventRequest", "StatusResponse")
	for _, target := range []struct{ scope, prefix string }{{"portal", "/api-keys"}, {"api", "/merchant-admin/merchants/:merchantId/api-keys"}} {
		add(target.scope, "GET", target.prefix, "", "MerchantAPIKeyListResponse")
		add(target.scope, "POST", target.prefix, "CreateMerchantAPIKeyRequest", "MerchantAPIKeyResponse")
		add(target.scope, "DELETE", target.prefix+"/:id", "", "")
	}
	for _, target := range []struct{ policy, networks string }{
		{"/security/network-policies/:surface", "/security/network-policies/:surface/allowed-networks"},
		{"/api-keys/:id/network-policy", "/api-keys/:id/allowed-networks"},
	} {
		portal("GET", target.policy, "", "MerchantNetworkPolicyResponse")
		portal("PUT", target.policy, "SetMerchantNetworkPolicyRequest", "MerchantNetworkPolicy")
		portal("POST", target.networks, "AddMerchantAllowedNetworkRequest", "MerchantNetworkPolicy")
		portal("DELETE", target.networks+"/:networkId", "RequiredReasonRequest", "MerchantNetworkPolicy")
	}
	const users = "/merchant-admin/merchants/:merchantId/users"
	add("api", "GET", users, "", "MerchantUserListResponse")
	add("api", "POST", users, "AddMerchantUserRequest", "")
	add("api", "DELETE", users+"/:id", "", "")
	add("api", "PATCH", users+"/:id/role", "UpdateMerchantUserRoleRequest", "")
	return slices.Concat(routes, entityRoutes(), automationRoutes(), systemRoutes(), securityRoutes(), serviceOperationRoutes())
}

func GenerateRoutes(contracts []Contract, routes []RouteContract) ([]byte, error) {
	types := make(map[string]Direction, len(contracts))
	shapes := make(map[string]reflect.Type, len(contracts))
	for _, contract := range contracts {
		types[contract.Name] = contract.Direction
		shapes[contract.Name] = contract.Type
	}
	seen := make(map[string]bool, len(routes))
	var output bytes.Buffer
	output.WriteString("// Code generated by go run ./cmd/http-contracts. DO NOT EDIT.\n")
	output.WriteString("import { z } from \"zod\"\nimport * as Wire from \"./http-contract-schemas\"\nimport type { HTTPRouteContract } from \"@/lib/http-contract-types\"\n\n")
	output.WriteString("export const httpRoutes = [\n")
	for _, route := range routes {
		key := route.Scope + " " + route.Method + " " + route.Path
		if seen[key] {
			return nil, fmt.Errorf("duplicate HTTP route contract %s", key)
		}
		seen[key] = true
		if (route.Scope != "api" && route.Scope != "portal") || !strings.HasPrefix(route.Path, "/") {
			return nil, fmt.Errorf("invalid HTTP route contract %s", key)
		}
		switch route.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return nil, fmt.Errorf("invalid HTTP method %s", route.Method)
		}
		schema := func(name string, direction Direction) (string, error) {
			if name == "" {
				return "null", nil
			}
			actual, ok := types[name]
			if !ok || actual != direction {
				return "", fmt.Errorf("route %s: missing %v contract %s", key, direction, name)
			}
			return "Wire." + name + "Schema", nil
		}
		request, err := schema(route.Request, Input)
		if err != nil {
			return nil, err
		}
		response, err := schema(route.Response, Output)
		if err != nil {
			return nil, err
		}
		if len(route.ResponseVariants) != 0 {
			if route.Response != "" || len(route.ResponseVariants) < 2 {
				return nil, fmt.Errorf("route %s: response variants require two or more concrete alternatives", key)
			}
			alternatives := make([]string, 0, len(route.ResponseVariants))
			for _, name := range route.ResponseVariants {
				validator, err := schema(name, Output)
				if err != nil || name == "" {
					return nil, fmt.Errorf("route %s: invalid response alternative %q", key, name)
				}
				alternatives = append(alternatives, validator)
			}
			response = "z.union([" + strings.Join(alternatives, ", ") + "])"
		}
		fields := "null"
		if route.Request != "" {
			names := requestFieldNames(shapes[route.Request])
			if names != nil {
				for index := range names {
					names[index] = strconv.Quote(names[index])
				}
				fields = "[" + strings.Join(names, ", ") + "]"
			}
		}
		fmt.Fprintf(&output, "  { scope: %s, method: %s, path: %s, request: %s, requestFields: %s, response: %s, optionalRequest: %t },\n", strconv.Quote(route.Scope), strconv.Quote(route.Method), strconv.Quote(route.Path), request, fields, response, route.OptionalRequest)
	}
	output.WriteString("] as const satisfies readonly HTTPRouteContract[]\n")
	return output.Bytes(), nil
}

// Request DTOs own writable names. Form projection must not infer these from
// display metadata or copy server-owned properties from an entity response.
func requestFieldNames(t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || hasCustomCodec(t) {
		return nil
	}
	names := []string{}
	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" {
			names = append(names, requestFieldNames(field.Type)...)
			continue
		}
		if name == "" {
			name = field.Name
		}
		names = append(names, name)
	}
	return names
}

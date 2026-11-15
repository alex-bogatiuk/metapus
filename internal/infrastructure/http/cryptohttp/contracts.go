package cryptohttp

import (
	"metapus/internal/application/cryptoapp"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/cryptohttp/cryptodto"
)

// HTTPContracts declares the wire contracts of the crypto routes that this
// module mounts, for the frontend contract generator and route checks.
func HTTPContracts() spec.Bundle {
	return spec.Bundle{Contracts: contracts(), Routes: routes()}
}

func contracts() []spec.Contract {
	return []spec.Contract{
		spec.Response[cryptodto.AdminDashboardQueuesResponse]("AdminDashboardQueuesResponse"),
		spec.Response[cryptodto.AdminDashboardFinanceResponse]("AdminDashboardFinanceResponse"),
		spec.Response[cryptodto.AdminDashboardAnalyticsResponse]("AdminDashboardAnalyticsResponse"),
		spec.Response[cryptodto.UnidentifiedReceiptCredit]("UnidentifiedReceiptCredit"),
		spec.Response[cryptodto.UnidentifiedReceiptItem]("UnidentifiedReceiptItem"),
		spec.Response[cryptodto.UnidentifiedReceiptListResponse]("UnidentifiedReceiptListResponse"),
		spec.Response[cryptodto.CustodyReceiptProofSource]("CustodyReceiptProofSource"),
		spec.Response[cryptodto.CustodyReceiptProof]("CustodyReceiptProof"),
		spec.Response[cryptodto.CustodyReceiptAudit]("CustodyReceiptAudit"),
		spec.Response[cryptodto.CustodyReceiptAttribution]("CustodyReceiptAttribution"),
		spec.Response[cryptodto.CustodyReceiptDetailResponse]("CustodyReceiptDetailResponse"),
		spec.Request[cryptodto.PrepareMerchantCreditRequest]("PrepareMerchantCreditRequest"),
		spec.Request[cryptodto.DecideMerchantCreditRequest]("DecideMerchantCreditRequest"),
		spec.Response[cryptodto.MerchantCreditResponse]("MerchantCreditResponse"),
		spec.Response[cryptodto.UnidentifiedReceiptPlatformFunding]("UnidentifiedReceiptPlatformFunding"),
		spec.Response[cryptodto.UnidentifiedReceiptRefund]("UnidentifiedReceiptRefund"),
		spec.Response[cryptodto.ReceiptMerchantTie]("ReceiptMerchantTie"),
		spec.Request[cryptodto.PreparePlatformFundingRequest]("PreparePlatformFundingRequest"),
		spec.Request[cryptodto.DecidePlatformFundingRequest]("DecidePlatformFundingRequest"),
		spec.Response[cryptodto.PlatformFundingResponse]("PlatformFundingResponse"),
		spec.Response[cryptodto.WalletFundingItem]("WalletFundingItem"),
		spec.Response[cryptodto.WalletFundingListResponse]("WalletFundingListResponse"),
		spec.Request[cryptodto.CreateTreasuryAddressRequest]("CreateTreasuryAddressRequest"),
		spec.Response[cryptodto.TreasuryAddressResponse]("TreasuryAddressResponse"),
		spec.Request[cryptodto.ApproveRejectRequest]("ApproveRejectRequest"),
		spec.Request[cryptodto.ProposeFinancialAdjustmentRequest]("ProposeFinancialAdjustmentRequest"),
		spec.Request[cryptodto.FinancialAdjustmentSourceRequest]("FinancialAdjustmentSourceRequest"),
		spec.Response[cryptodto.FinancialAdjustmentSourceResponse]("FinancialAdjustmentSourceResponse"),
		spec.Response[cryptodto.FinancialAdjustmentPaymentSourcesResponse]("FinancialAdjustmentPaymentSourcesResponse"),
		spec.Response[cryptodto.FinancialAdjustmentResponse]("FinancialAdjustmentResponse"),
		spec.Response[cryptodto.FinancialAdjustmentListResponse]("FinancialAdjustmentListResponse"),
		spec.Response[cryptodto.TreasuryAvailabilityResponse]("TreasuryAvailabilityResponse"),
		spec.Response[cryptodto.TreasurySourceWalletResponse]("TreasurySourceWalletResponse"),
		spec.Response[cryptodto.ExternalOutflowResponse]("ExternalOutflowResponse"),
		spec.Response[cryptodto.ExternalOutflowActionResponse]("ExternalOutflowActionResponse"),
		spec.Response[cryptodto.OperationalHoldResponse]("OperationalHoldResponse"),
		spec.Response[cryptodto.CustodyAdjustmentResponse]("CustodyAdjustmentResponse"),
		spec.Request[cryptodto.ClassifyExternalOutflowRequest]("ClassifyExternalOutflowRequest"),
		spec.Response[cryptodto.TreasuryAddressListResponse]("TreasuryAddressListResponse"),
		spec.Response[cryptodto.TreasurySourceWalletListResponse]("TreasurySourceWalletListResponse"),
		spec.Response[cryptodto.ExternalOutflowListResponse]("ExternalOutflowListResponse"),
		spec.Response[cryptodto.ExternalOutflowDetailResponse]("ExternalOutflowDetailResponse"),
		spec.Response[cryptodto.WebhookEndpointListResponse]("WebhookEndpointListResponse"),
		spec.Response[cryptodto.MerchantEventListResponse]("MerchantEventListResponse"),
		spec.Response[cryptodto.MerchantEventDeliveriesResponse]("MerchantEventDeliveriesResponse"),
		spec.Response[cryptodto.WebhookTestResponse]("WebhookTestResponse"),
		spec.Response[cryptodto.PortalMerchantListResponse]("PortalMerchantListResponse"),
		spec.Response[cryptodto.PortalCurrencyListResponse]("PortalCurrencyListResponse"),
		spec.Response[cryptodto.PortalChartResponse]("PortalChartResponse"),
		spec.Response[cryptodto.MerchantAPIKeyListResponse]("MerchantAPIKeyListResponse"),
		spec.Response[cryptodto.MerchantUserListResponse]("MerchantUserListResponse"),
		spec.Response[cryptodto.PortalWithdrawalAddressCreatedResponse]("PortalWithdrawalAddressCreatedResponse"),
		spec.Response[cryptodto.PortalWithdrawalCreatedResponse]("PortalWithdrawalCreatedResponse"),
		spec.Response[cryptodto.StatusResponse]("StatusResponse"),
		spec.Response[cryptodto.SigningJobStatusResponse]("SigningJobStatusResponse"),
		spec.Request[cryptodto.ReasonRequest]("ReasonRequest"),
		spec.Request[cryptodto.SetMerchantNetworkPolicyRequest]("SetMerchantNetworkPolicyRequest"),
		spec.Request[cryptodto.AddMerchantAllowedNetworkRequest]("AddMerchantAllowedNetworkRequest"),
		spec.Response[cryptodto.MerchantNetworkPolicyResponse]("MerchantNetworkPolicyResponse"),
		spec.Response[cryptodto.MerchantNetworkPolicy]("MerchantNetworkPolicy"),
		spec.Request[cryptodto.RequiredReasonRequest]("RequiredReasonRequest"),
		spec.Request[cryptodto.AddMerchantUserRequest]("AddMerchantUserRequest"),
		spec.Response[cryptodto.AdminApproveWithdrawalResponse]("AdminApproveWithdrawalResponse"),
		spec.Response[cryptodto.AdminWithdrawalRequestItem]("AdminWithdrawalRequestItem"),
		spec.Request[cryptodto.CreateMerchantAPIKeyRequest]("CreateMerchantAPIKeyRequest"),
		spec.Request[cryptodto.CreateMerchantAddressRequest]("CreateMerchantAddressRequest"),
		spec.Request[cryptodto.CreateMerchantInvoiceRequest]("CreateMerchantInvoiceRequest"),
		spec.Request[cryptodto.BindMerchantInvoiceReferenceRequest]("BindMerchantInvoiceReferenceRequest"),
		spec.Request[cryptodto.CreatePaymentLinkRequest]("CreatePaymentLinkRequest"),
		spec.Request[cryptodto.CryptoRPCEndpointFields]("CryptoRPCEndpointFields"),
		spec.Request[cryptodto.CreateWebhookEndpointRequest]("CreateWebhookEndpointRequest"),
		spec.Response[cryptodto.CryptoNetworkPolicyResponse]("CryptoNetworkPolicyResponse"),
		spec.Response[cryptodto.CryptoNetworkStateResponse]("CryptoNetworkStateResponse"),
		spec.Response[cryptodto.CryptoShadowControlResponse]("CryptoShadowControlResponse"),
		spec.Response[cryptodto.CryptoRPCConfigurationResponse]("CryptoRPCConfigurationResponse"),
		spec.Request[cryptodto.UpdateCryptoRPCConfigurationRequest]("UpdateCryptoRPCConfigurationRequest"),
		spec.Request[cryptodto.CryptoRPCConfigurationChange]("CryptoRPCConfigurationChange"),
		spec.Request[cryptodto.SetCryptoShadowControlRequest]("SetCryptoShadowControlRequest"),
		spec.Request[cryptodto.SetCryptoNetworkWatchingRequest]("SetCryptoNetworkWatchingRequest"),
		spec.Response[cryptodto.CryptoProviderBalanceOperationDetail]("CryptoProviderBalanceOperationDetail"),
		spec.Request[cryptodto.ExternalResourcePolicyRequest]("ExternalResourcePolicyRequest"),
		spec.Response[cryptodto.ExternalResourcePolicyResponse]("ExternalResourcePolicyResponse"),
		spec.Request[cryptodto.FeeFundingPolicyRequest]("FeeFundingPolicyRequest"),
		spec.Response[cryptodto.FeeFundingPolicyResponse]("FeeFundingPolicyResponse"),
		spec.Request[cryptodto.FeeScheduleDeleteRequest]("FeeScheduleDeleteRequest"),
		spec.Request[cryptodto.FeeScheduleRequest]("FeeScheduleRequest"),
		spec.Response[cryptodto.FeeScheduleResponse]("FeeScheduleResponse"),
		spec.Response[cryptodto.MerchantAPIKeyListItem]("MerchantAPIKeyListItem"),
		spec.Response[cryptodto.MerchantAPIKeyResponse]("MerchantAPIKeyResponse"),
		spec.Response[cryptodto.MerchantAddressResponse]("MerchantAddressResponse"),
		spec.Response[cryptodto.MerchantCurrencyItem]("MerchantCurrencyItem"),
		spec.Response[cryptodto.MerchantCurrencyListResponse]("MerchantCurrencyListResponse"),
		spec.Response[cryptodto.MerchantEventResponse]("MerchantEventResponse"),
		spec.Response[cryptodto.MerchantInvoiceListResponse]("MerchantInvoiceListResponse"),
		spec.Response[cryptodto.MerchantInvoiceResponse]("MerchantInvoiceResponse"),
		spec.Response[cryptodto.MerchantPaymentResponse]("MerchantPaymentResponse"),
		spec.Response[cryptodto.MerchantUserItem]("MerchantUserItem"),
		spec.Response[cryptodto.MerchantWalletAllocationLimitsResponse]("MerchantWalletAllocationLimitsResponse"),
		spec.Request[cryptodto.PortalAddWithdrawalAddressRequest]("PortalAddWithdrawalAddressRequest"),
		spec.Response[cryptodto.PortalBalanceResponse]("PortalBalanceResponse"),
		spec.Response[cryptodto.PortalChartPoint]("PortalChartPoint"),
		spec.Request[cryptodto.PortalCreateDepositAddressRequest]("PortalCreateDepositAddressRequest"),
		spec.Response[cryptodto.PortalCreateDepositAddressResponse]("PortalCreateDepositAddressResponse"),
		spec.Request[cryptodto.PortalRotateDepositAddressRequest]("PortalRotateDepositAddressRequest"),
		spec.Response[cryptodto.PortalRotateDepositAddressResponse]("PortalRotateDepositAddressResponse"),
		spec.Request[cryptodto.PortalCreateInvoiceRequest]("PortalCreateInvoiceRequest"),
		spec.Response[cryptodto.PortalCreateInvoiceResponse]("PortalCreateInvoiceResponse"),
		spec.Request[cryptodto.PortalCreateWithdrawalRequest]("PortalCreateWithdrawalRequest"),
		spec.Response[cryptodto.PortalRefundablePaymentResponse]("PortalRefundablePaymentResponse"),
		spec.Response[cryptodto.PortalWithdrawalSource]("PortalWithdrawalSource"),
		spec.Response[cryptodto.PortalCurrencyItem]("PortalCurrencyItem"),
		spec.Response[cryptodto.PortalDepositAddressItem]("PortalDepositAddressItem"),
		spec.Response[cryptodto.PortalDepositAddressListResponse]("PortalDepositAddressListResponse"),
		spec.Response[cryptodto.PortalDepositAddressQuotaItem]("PortalDepositAddressQuotaItem"),
		spec.Response[cryptodto.PortalDetailedBalanceResponse]("PortalDetailedBalanceResponse"),
		spec.Response[cryptodto.PortalRiskBalanceResponse]("PortalRiskBalanceResponse"),
		spec.Response[cryptodto.PortalFeeItem]("PortalFeeItem"),
		spec.Response[cryptodto.PortalFeeScheduleResponse]("PortalFeeScheduleResponse"),
		spec.Response[cryptodto.PortalFunnelResponse]("PortalFunnelResponse"),
		spec.Response[cryptodto.PortalInvoiceDetailResponse]("PortalInvoiceDetailResponse"),
		spec.Response[cryptodto.PortalInvoiceLedger]("PortalInvoiceLedger"),
		spec.Response[cryptodto.PortalInvoiceFinancialSummary]("PortalInvoiceFinancialSummary"),
		spec.Response[cryptodto.PortalInvoiceItem]("PortalInvoiceItem"),
		spec.Response[cryptodto.PortalInvoiceListResponse]("PortalInvoiceListResponse"),
		spec.Response[cryptodto.PortalInvoicePayment]("PortalInvoicePayment"),
		spec.Response[cryptodto.PortalMerchantItem]("PortalMerchantItem"),
		spec.Response[cryptodto.PortalPaymentLinkCreateResponse]("PortalPaymentLinkCreateResponse"),
		spec.Response[cryptodto.PortalPaymentLinkItem]("PortalPaymentLinkItem"),
		spec.Response[cryptodto.PortalPaymentLinkListResponse]("PortalPaymentLinkListResponse"),
		spec.Response[cryptodto.PortalSettingsResponse]("PortalSettingsResponse"),
		spec.Response[cryptodto.PortalSummaryResponse]("PortalSummaryResponse"),
		spec.Response[cryptodto.PortalReceiptTotal]("PortalReceiptTotal"),
		spec.Response[cryptodto.PortalTimelineEvent]("PortalTimelineEvent"),
		spec.Response[cryptodto.PortalTimelineEventMetadata]("PortalTimelineEventMetadata"),
		spec.Response[cryptodto.PortalTokenBalance]("PortalTokenBalance"),
		spec.Response[cryptodto.PortalTokenDetailed]("PortalTokenDetailed"),
		spec.Response[cryptodto.PortalWebhookDeliveryItem]("PortalWebhookDeliveryItem"),
		spec.Response[cryptodto.PortalWithdrawalAddress]("PortalWithdrawalAddress"),
		spec.Response[cryptodto.PortalWithdrawalAddressListResponse]("PortalWithdrawalAddressListResponse"),
		spec.Response[cryptodto.PortalWithdrawalItem]("PortalWithdrawalItem"),
		spec.Response[cryptodto.PortalWithdrawalListResponse]("PortalWithdrawalListResponse"),
		spec.Response[cryptodto.PortalWithdrawalRequestItem]("PortalWithdrawalRequestItem"),
		spec.Response[cryptodto.PortalWithdrawalRequestListResponse]("PortalWithdrawalRequestListResponse"),
		spec.Request[cryptodto.RedeliverMerchantEventRequest]("RedeliverMerchantEventRequest"),
		spec.Request[cryptodto.UpdateCryptoNetworkPolicyRequest]("UpdateCryptoNetworkPolicyRequest"),
		spec.Request[cryptodto.UpdateMerchantUserRoleRequest]("UpdateMerchantUserRoleRequest"),
		spec.Request[cryptodto.UpdateMerchantWalletAllocationLimitsRequest]("UpdateMerchantWalletAllocationLimitsRequest"),
		spec.Request[cryptodto.UpdatePortalSettingsRequest]("UpdatePortalSettingsRequest"),
		spec.Request[cryptodto.UpdateWalletPoolActivationPolicyRequest]("UpdateWalletPoolActivationPolicyRequest"),
		spec.Request[cryptodto.UpdateWalletPoolPolicyRequest]("UpdateWalletPoolPolicyRequest"),
		spec.Request[cryptodto.UpdateWebhookEndpointRequest]("UpdateWebhookEndpointRequest"),
		spec.Response[cryptodto.WalletPoolActivationPolicyResponse]("WalletPoolActivationPolicyResponse"),
		spec.Response[cryptodto.WalletPoolCapabilitiesResponse]("WalletPoolCapabilitiesResponse"),
		spec.Response[cryptodto.WalletPoolListResponse]("WalletPoolListResponse"),
		spec.Response[cryptodto.WalletPoolPlanResultResponse]("WalletPoolPlanResultResponse"),
		spec.Response[cryptodto.WalletPoolPolicyResponse]("WalletPoolPolicyResponse"),
		spec.Response[cryptodto.WalletPoolReconcileResponse]("WalletPoolReconcileResponse"),
		spec.Response[cryptodto.WalletPoolStateResponse]("WalletPoolStateResponse"),
		spec.Response[cryptodto.WalletPoolStatsResponse]("WalletPoolStatsResponse"),
		spec.Response[cryptodto.WebhookEndpointResponse]("WebhookEndpointResponse"),
		spec.Response[cryptodto.WithdrawalRequestUSDValuation]("WithdrawalRequestUSDValuation"),
		spec.Response[cryptodto.CryptoNetworkListResponse]("CryptoNetworkListResponse"),
		spec.Request[cryptodto.PrepareProviderOpeningRequest]("PrepareProviderOpeningRequest"),
		spec.Request[cryptodto.ProviderBalanceDecisionRequest]("ProviderBalanceDecisionRequest"),
		spec.Response[cryptoapp.ProviderBalanceResult]("ProviderBalanceDecisionResponse"),
		spec.Response[cryptodto.FeeScheduleListResponse]("FeeScheduleListResponse"),
	}
}

func routes() []spec.RouteContract {
	routes := []spec.RouteContract{}
	add := func(scope, method, path, request, response string) {
		routes = append(routes, spec.RouteContract{Scope: scope, Method: method, Path: path, Request: request, Response: response})
	}
	add("api", "GET", "/crypto/unidentified-receipts", "", "UnidentifiedReceiptListResponse")
	add("api", "GET", "/crypto/unidentified-receipts/:id", "", "CustodyReceiptDetailResponse")
	add("api", "POST", "/crypto/unidentified-receipts/:id/credits", "PrepareMerchantCreditRequest", "MerchantCreditResponse")
	add("api", "POST", "/crypto/merchant-credits/:id/approve", "DecideMerchantCreditRequest", "MerchantCreditResponse")
	add("api", "POST", "/crypto/merchant-credits/:id/reject", "DecideMerchantCreditRequest", "MerchantCreditResponse")
	add("api", "POST", "/crypto/unidentified-receipts/:id/platform-fundings", "PreparePlatformFundingRequest", "PlatformFundingResponse")
	add("api", "GET", "/crypto/wallet-fundings", "", "WalletFundingListResponse")
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
	portal("GET", "/payments/:id/refund", "", "PortalRefundablePaymentResponse")
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
	api := func(method, path, request, response string) { add("api", method, path, request, response) }
	api("GET", "/crypto/wallet-pools", "", "WalletPoolListResponse")
	api("PATCH", "/crypto/wallet-pools/:networkId/policy", "UpdateWalletPoolPolicyRequest", "WalletPoolStateResponse")
	api("POST", "/crypto/wallet-pools/:networkId/reconcile", "", "WalletPoolReconcileResponse")
	api("GET", "/crypto/wallet-pools/:networkId/merchant-overrides/:merchantId", "", "MerchantWalletAllocationLimitsResponse")
	api("PUT", "/crypto/wallet-pools/:networkId/merchant-overrides/:merchantId", "UpdateMerchantWalletAllocationLimitsRequest", "MerchantWalletAllocationLimitsResponse")
	api("GET", "/admin/crypto/networks", "", "CryptoNetworkListResponse")
	api("GET", "/admin/crypto/networks/:networkId/status", "", "CryptoNetworkStateResponse")
	api("GET", "/admin/crypto/networks/:networkId/rpc-configuration", "", "CryptoRPCConfigurationResponse")
	api("PATCH", "/admin/crypto/networks/:networkId/rpc-configuration", "UpdateCryptoRPCConfigurationRequest", "CryptoRPCConfigurationResponse")
	api("PATCH", "/admin/crypto/networks/:networkId/policy", "UpdateCryptoNetworkPolicyRequest", "CryptoNetworkStateResponse")
	api("POST", "/admin/crypto/networks/:networkId/watching", "SetCryptoNetworkWatchingRequest", "CryptoNetworkStateResponse")
	api("GET", "/admin/crypto/networks/:networkId/shadow", "", "CryptoShadowControlResponse")
	api("POST", "/admin/crypto/networks/:networkId/shadow", "SetCryptoShadowControlRequest", "CryptoShadowControlResponse")
	for _, path := range []string{"/system/fee-schedule", "/merchant-admin/merchants/:merchantId/fee-schedule"} {
		api("GET", path, "", "FeeScheduleListResponse")
		api("PUT", path, "FeeScheduleRequest", "")
		api("DELETE", path, "FeeScheduleDeleteRequest", "")
	}
	routes = append(routes, spec.RouteContract{Scope: "portal", Method: "GET", Path: "/invoices/export"})
	return routes
}

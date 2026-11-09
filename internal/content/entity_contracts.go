package content

import (
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/metadata"
)

func (r *RateSourceRegistration) HTTPContracts() spec.Bundle {
	b := r.CatalogRegistrationFor.HTTPContracts()
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/mappings", "", "ExchangeRateMappingsResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/feed-state", "", "ExchangeRateFeedStateResponse")
	b.AddRoute(r.EntityDefinition(), "PUT", "/:id/mappings/:currencyId/:baseCurrencyId", "UpdateRateMappingRequest", "ExchangeRateMappingResponse")

	return b
}

func (r *CryptoNetworkOperationRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[dto.CreateCryptoNetworkOperationRequest, dto.UpdateCryptoNetworkOperationRequest, dto.CryptoNetworkOperationResponse, dto.CryptoNetworkOperationResponse](r.EntityDefinition(), r.DocumentCapabilities())
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/allow-editing", "", "CryptoNetworkOperationResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/deny-editing", "", "CryptoNetworkOperationResponse")

	return b
}

func (r *CryptoProviderBalanceOperationRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[dto.CreateCryptoProviderBalanceOperationRequest, dto.UpdateCryptoProviderBalanceOperationRequest, dto.CryptoProviderBalanceOperationResponse, dto.CryptoProviderBalanceOperationDetail](r.EntityDefinition(), r.DocumentCapabilities())
	b.AddRoute(r.EntityDefinition(), "POST", "/provider-balance-incidents/:incidentId/prepare-opening", "PrepareProviderOpeningRequest", "ProviderBalanceDecisionResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/approve", "ProviderBalanceDecisionRequest", "ProviderBalanceDecisionResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/reject", "ProviderBalanceDecisionRequest", "ProviderBalanceDecisionResponse")

	return b
}

func (r *CryptoResourceRentalRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[dto.CreateCryptoResourceRentalRequest, dto.UpdateCryptoResourceRentalRequest, dto.CryptoResourceRentalResponse, dto.CryptoResourceRentalResponse](r.EntityDefinition(), r.DocumentCapabilities())

	b.Contracts = append(b.Contracts, spec.Response[dto.ResourceRentalMetricsResponse]("ResourceRentalMetricsResponse"), spec.Response[dto.ProviderBalanceIncidentsResponse]("ProviderBalanceIncidentsResponse"), spec.Response[dto.ResourceRentalAllocationsResponse]("ResourceRentalAllocationsResponse"))
	b.AddRoute(r.EntityDefinition(), "GET", "/metrics", "", "ResourceRentalMetricsResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/provider-balance-incidents", "", "ProviderBalanceIncidentsResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/allocations", "", "ResourceRentalAllocationsResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/provider-balance-incidents/:incidentId/acknowledge", "", "SuccessResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/reclaim", "", "SuccessResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/retry", "", "SuccessResponse")

	return b
}

func (r *CryptoInvoiceRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[dto.CreateCryptoInvoiceRequest, dto.UpdateCryptoInvoiceRequest, dto.CryptoInvoiceResponse, dto.CryptoInvoiceResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *CryptoPaymentRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[dto.CreateCryptoPaymentRequest, dto.UpdateCryptoPaymentRequest, dto.CryptoPaymentResponse, dto.CryptoPaymentResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *CryptoWithdrawalRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[dto.CreateCryptoWithdrawalRequest, dto.UpdateCryptoWithdrawalRequest, dto.CryptoWithdrawalResponse, dto.CryptoWithdrawalResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *CryptoSweepRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[dto.CreateCryptoSweepRequest, dto.UpdateCryptoSweepRequest, dto.CryptoSweepResponse, dto.CryptoSweepDetailResponse](r.EntityDefinition(), r.DocumentCapabilities())
	b.Contracts = append(b.Contracts, spec.Response[dto.CryptoSweepTokenDisplay]("CryptoSweepTokenDisplay"))
	b.Contracts = append(b.Contracts, spec.Response[dto.CryptoSweepWalletDisplay]("CryptoSweepWalletDisplay"))
	b.Contracts = append(b.Contracts, spec.Response[dto.CryptoSweepLineResponse]("CryptoSweepLineResponse"))
	b.Contracts = append(b.Contracts, spec.Response[dto.CryptoSweepNetworkFeeResponse]("CryptoSweepNetworkFeeResponse"))
	b.Contracts = append(b.Contracts, spec.Response[dto.CryptoSweepResourceCostResponse]("CryptoSweepResourceCostResponse"))
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/retry", "", "SuccessResponse")

	return b
}

func (r *TreasuryWithdrawalRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[dto.CreateTreasuryWithdrawalRequest, dto.UpdateTreasuryWithdrawalRequest, dto.TreasuryWithdrawalResponse, dto.TreasuryWithdrawalResponse](r.EntityDefinition(), r.DocumentCapabilities())
	b.AddRoute(r.EntityDefinition(), "GET", "/addresses", "", "TreasuryAddressListResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/addresses", "CreateTreasuryAddressRequest", "IDResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/availability", "", "TreasuryAvailabilityResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/source-wallets", "", "TreasurySourceWalletListResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/approve", "ApproveRejectRequest", "SuccessResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/reject", "ApproveRejectRequest", "SuccessResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/signing-job", "", "SigningJobStatusResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/retry-signing", "", "StatusResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/reconciliation/outflows", "", "ExternalOutflowListResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/reconciliation/outflows/:outflowId", "", "ExternalOutflowDetailResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/reconciliation/outflows/:outflowId/classify", "ClassifyExternalOutflowRequest", "ExternalOutflowDetailResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/reconciliation/outflows/:outflowId/resolve", "ApproveRejectRequest", "ExternalOutflowDetailResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/reconciliation/custody-adjustments/:adjustmentId/approve", "ApproveRejectRequest", "CustodyAdjustmentResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/reconciliation/custody-adjustments/:adjustmentId/reject", "ApproveRejectRequest", "CustodyAdjustmentResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/reconciliation/financial-adjustments", "", "FinancialAdjustmentListResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/reconciliation/financial-adjustments/payment/:paymentId/sources", "", "FinancialAdjustmentPaymentSourcesResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/reconciliation/financial-adjustments", "ProposeFinancialAdjustmentRequest", "FinancialAdjustmentResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/reconciliation/financial-adjustments/:adjustmentId", "", "FinancialAdjustmentResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/reconciliation/financial-adjustments/:adjustmentId/approve", "ApproveRejectRequest", "FinancialAdjustmentResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/reconciliation/financial-adjustments/:adjustmentId/reject", "ApproveRejectRequest", "FinancialAdjustmentResponse")

	return b
}

func (r *WithdrawalRequestRegistration) HTTPContracts() spec.Bundle {
	b := spec.Bundle{Contracts: []spec.Contract{spec.Response[dto.AdminWithdrawalRequestDetail]("AdminWithdrawalRequestDetail"), spec.Response[dto.AdminWithdrawalRequestListResponse]("AdminWithdrawalRequestListResponse")}}
	b.AddRoute(r.EntityDefinition(), "GET", "", "", "AdminWithdrawalRequestListResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/export-list", "ExportListRequest", "")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id", "", "AdminWithdrawalRequestDetail")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/related-documents", "", "RelatedDocumentsResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/movements", "", "DocumentMovementsResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/approve", "", "AdminApproveWithdrawalResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/reject", "ReasonRequest", "StatusResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/signing-job", "", "SigningJobStatusResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/retry-signing", "", "StatusResponse")

	return b
}

func (*CryptoInvoiceRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true, Post: true, Unpost: true, Repost: true, RelatedDocuments: true, Movements: true, BatchAction: true, BatchActionByFilter: true, ExportList: true}
}

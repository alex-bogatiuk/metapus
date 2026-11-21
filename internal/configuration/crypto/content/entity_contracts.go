package content

import (
	"metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"metapus/internal/infrastructure/http/contracts/spec"
	"metapus/internal/metadata"
)

func (r *CryptoNetworkOperationRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[struct{}, struct{}, cryptodto.CryptoNetworkOperationResponse, cryptodto.CryptoNetworkOperationResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *CryptoProviderBalanceOperationRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[struct{}, struct{}, cryptodto.CryptoProviderBalanceOperationResponse, cryptodto.CryptoProviderBalanceOperationDetail](r.EntityDefinition(), r.DocumentCapabilities())
	b.AddRoute(r.EntityDefinition(), "POST", "/provider-balance-incidents/:incidentId/prepare-opening", "PrepareProviderOpeningRequest", "ProviderBalanceDecisionResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/approve", "ProviderBalanceDecisionRequest", "ProviderBalanceDecisionResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/reject", "ProviderBalanceDecisionRequest", "ProviderBalanceDecisionResponse")

	return b
}

func (r *CryptoResourceRentalRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[struct{}, struct{}, cryptodto.CryptoResourceRentalResponse, cryptodto.CryptoResourceRentalResponse](r.EntityDefinition(), r.DocumentCapabilities())

	b.Contracts = append(b.Contracts, spec.Response[cryptodto.ResourceRentalMetricsResponse]("ResourceRentalMetricsResponse"), spec.Response[cryptodto.ProviderBalanceIncidentsResponse]("ProviderBalanceIncidentsResponse"), spec.Response[cryptodto.ResourceRentalAllocationsResponse]("ResourceRentalAllocationsResponse"))
	b.AddRoute(r.EntityDefinition(), "GET", "/metrics", "", "ResourceRentalMetricsResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/provider-balance-incidents", "", "ProviderBalanceIncidentsResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/:id/allocations", "", "ResourceRentalAllocationsResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/provider-balance-incidents/:incidentId/acknowledge", "", "SuccessResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/reclaim", "", "SuccessResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/retry", "", "SuccessResponse")

	return b
}

func (r *CryptoInvoiceRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[cryptodto.CreateCryptoInvoiceRequest, cryptodto.UpdateCryptoInvoiceRequest, cryptodto.CryptoInvoiceResponse, cryptodto.CryptoInvoiceResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *CryptoPaymentRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[cryptodto.CreateCryptoPaymentRequest, cryptodto.UpdateCryptoPaymentRequest, cryptodto.CryptoPaymentResponse, cryptodto.CryptoPaymentResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *CryptoWithdrawalRegistration) HTTPContracts() spec.Bundle {
	return spec.EntityWithDetail[cryptodto.CreateCryptoWithdrawalRequest, cryptodto.UpdateCryptoWithdrawalRequest, cryptodto.CryptoWithdrawalResponse, cryptodto.CryptoWithdrawalResponse](r.EntityDefinition(), r.DocumentCapabilities())
}

func (r *CryptoSweepRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[cryptodto.CreateCryptoSweepRequest, cryptodto.UpdateCryptoSweepRequest, cryptodto.CryptoSweepResponse, cryptodto.CryptoSweepDetailResponse](r.EntityDefinition(), r.DocumentCapabilities())
	b.Contracts = append(b.Contracts, spec.Response[cryptodto.CryptoSweepTokenDisplay]("CryptoSweepTokenDisplay"))
	b.Contracts = append(b.Contracts, spec.Response[cryptodto.CryptoSweepWalletDisplay]("CryptoSweepWalletDisplay"))
	b.Contracts = append(b.Contracts, spec.Response[cryptodto.CryptoSweepLineResponse]("CryptoSweepLineResponse"))
	b.Contracts = append(b.Contracts, spec.Response[cryptodto.CryptoSweepNetworkFeeResponse]("CryptoSweepNetworkFeeResponse"))
	b.Contracts = append(b.Contracts, spec.Response[cryptodto.CryptoSweepResourceCostResponse]("CryptoSweepResourceCostResponse"))
	b.AddRoute(r.EntityDefinition(), "POST", "/:id/retry", "", "SuccessResponse")

	return b
}

func (r *TreasuryWithdrawalRegistration) HTTPContracts() spec.Bundle {
	b := spec.EntityWithDetail[cryptodto.CreateTreasuryWithdrawalRequest, cryptodto.UpdateTreasuryWithdrawalRequest, cryptodto.TreasuryWithdrawalResponse, cryptodto.TreasuryWithdrawalResponse](r.EntityDefinition(), r.DocumentCapabilities())
	b.AddRoute(r.EntityDefinition(), "GET", "/addresses", "", "TreasuryAddressListResponse")
	b.AddRoute(r.EntityDefinition(), "POST", "/addresses", "CreateTreasuryAddressRequest", "IDResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/availability", "", "TreasuryAvailabilityResponse")
	b.AddRoute(r.EntityDefinition(), "GET", "/source-wallets", "", "TreasurySourceWalletListResponse")
	b.Contracts = append(b.Contracts, spec.Request[cryptodto.CreateReceiptRefundRequest]("CreateReceiptRefundRequest"))
	b.AddRoute(r.EntityDefinition(), "POST", "/receipt-refunds", "CreateReceiptRefundRequest", "TreasuryWithdrawalResponse")
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
	b := spec.Bundle{Contracts: []spec.Contract{spec.Response[cryptodto.AdminWithdrawalRequestDetail]("AdminWithdrawalRequestDetail"), spec.Response[cryptodto.AdminWithdrawalRequestListResponse]("AdminWithdrawalRequestListResponse")}}
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
	return metadata.DocumentCapabilities{List: true, Read: true, Create: true, Update: true, Delete: true, SetDeletionMark: true, Post: true, Unpost: true, Repost: true, RelatedDocuments: true, Movements: true, PrintForms: true, BatchAction: true, BatchActionByFilter: true, ExportList: true}
}

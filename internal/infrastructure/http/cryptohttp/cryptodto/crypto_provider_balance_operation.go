package cryptodto

import (
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/infrastructure/http/v1/dto"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoProviderBalanceOperationResponse is a maker-checker correction of a
// provider balance; the evidence payload stays internal.
type CryptoProviderBalanceOperationResponse struct {
	dto.DocumentResponse
	ProviderAccountID     string     `json:"providerAccountId"`
	TokenID               string     `json:"tokenId"`
	ProviderObservationID *string    `json:"providerObservationId,omitempty"`
	SourceWalletID        *string    `json:"sourceWalletId,omitempty"`
	OperationType         string     `json:"operationType"`
	RecordType            string     `json:"recordType"`
	Amount                string     `json:"amount"`
	TxHash                string     `json:"txHash,omitempty"`
	ExternalRef           string     `json:"externalRef"`
	Status                string     `json:"status"`
	ApprovedBy            *string    `json:"approvedBy,omitempty"`
	ApprovedAt            *time.Time `json:"approvedAt,omitempty"`
	ApprovalReason        string     `json:"approvalReason,omitempty"`
	RejectedBy            *string    `json:"rejectedBy,omitempty"`
	RejectedAt            *time.Time `json:"rejectedAt,omitempty"`
	RejectionReason       string     `json:"rejectionReason,omitempty"`
	DecisionMode          string     `json:"decisionMode,omitempty"`
	DecisionPolicyVersion int        `json:"decisionPolicyVersion,omitzero"`

	Token        *postgres.RefDisplay `json:"token,omitempty"`
	SourceWallet *postgres.RefDisplay `json:"sourceWallet,omitempty"`
}

func CollectCryptoProviderBalanceOperationRefs(resolver *postgres.ReferenceResolver, o *crypto_provider_balance_operation.CryptoProviderBalanceOperation) {
	resolver.Add(TableTokens, o.TokenID)
	resolver.AddPtr(TableWallets, o.SourceWalletID)
}

func FromCryptoProviderBalanceOperation(o *crypto_provider_balance_operation.CryptoProviderBalanceOperation, refs postgres.ResolvedRefs) CryptoProviderBalanceOperationResponse {
	resp := CryptoProviderBalanceOperationResponse{
		DocumentResponse:  documentResponse(o.Document),
		ProviderAccountID: o.ProviderAccountID.String(), TokenID: o.TokenID.String(),
		ProviderObservationID: optionalID(o.ProviderObservationID), SourceWalletID: optionalID(o.SourceWalletID),
		OperationType: o.OperationType, RecordType: string(o.RecordType), Amount: o.Amount.String(), TxHash: o.TxHash,
		ExternalRef: o.ExternalRef, Status: o.Status, ApprovedBy: optionalID(o.ApprovedBy), ApprovedAt: o.ApprovedAt,
		ApprovalReason: o.ApprovalReason, RejectedBy: optionalID(o.RejectedBy), RejectedAt: o.RejectedAt,
		RejectionReason: o.RejectionReason, DecisionMode: string(o.DecisionMode), DecisionPolicyVersion: o.DecisionPolicyVersion,
	}
	if refs != nil {
		token := refs.Get(TableTokens, o.TokenID)
		resp.Token = &token
		resp.SourceWallet = refs.GetPtr(TableWallets, o.SourceWalletID)
	}
	return resp
}

// CryptoProviderBalanceOperationDetail adds the decisions this reader may take.
type CryptoProviderBalanceOperationDetail struct {
	CryptoProviderBalanceOperationResponse
	ActionAvailability map[string]entity.ActionAvailability `json:"actionAvailability"`
}

type PrepareProviderOpeningRequest struct {
	ObservationID id.ID  `json:"observationId" binding:"required"`
	Reason        string `json:"reason" binding:"required"`
}

type ProviderBalanceDecisionRequest struct {
	Version int    `json:"version" binding:"required,min=1"`
	Reason  string `json:"reason" binding:"required"`
}

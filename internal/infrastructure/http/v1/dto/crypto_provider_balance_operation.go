package dto

import (
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_provider_balance_operation"
)

type CreateCryptoProviderBalanceOperationRequest struct {
	ProviderAccountID     string            `json:"providerAccountId" binding:"required"`
	TokenID               string            `json:"tokenId" binding:"required"`
	ProviderObservationID *string           `json:"providerObservationId"`
	SourceWalletID        *string           `json:"sourceWalletId"`
	OperationType         string            `json:"operationType" binding:"required"`
	RecordType            entity.RecordType `json:"recordType" binding:"required"`
	Amount                string            `json:"amount" binding:"required"`
	TxHash                string            `json:"txHash"`
	ExternalRef           string            `json:"externalRef" binding:"required"`
	Evidence              map[string]any    `json:"evidence"`
}

func (r CreateCryptoProviderBalanceOperationRequest) ToEntity() *crypto_provider_balance_operation.CryptoProviderBalanceOperation {
	providerID, _ := id.Parse(r.ProviderAccountID)
	tokenID, _ := id.Parse(r.TokenID)
	amount, _ := types.NewCryptoAmountFromString(r.Amount)
	evidence, _ := json.Marshal(r.Evidence)
	doc := &crypto_provider_balance_operation.CryptoProviderBalanceOperation{
		Document: entity.NewDocument(), ProviderAccountID: providerID, TokenID: tokenID,
		OperationType: r.OperationType, RecordType: r.RecordType, Amount: amount,
		TxHash: r.TxHash, ExternalRef: r.ExternalRef, Evidence: evidence,
		Status: crypto_provider_balance_operation.StatusPendingApproval,
	}
	if r.SourceWalletID != nil {
		parsed, _ := id.Parse(*r.SourceWalletID)
		doc.SourceWalletID = &parsed
	}
	if r.ProviderObservationID != nil {
		parsed, _ := id.Parse(*r.ProviderObservationID)
		doc.ProviderObservationID = &parsed
	}
	return doc
}

type UpdateCryptoProviderBalanceOperationRequest struct {
	CreateCryptoProviderBalanceOperationRequest
	Version int `json:"version" binding:"required"`
}

func (r UpdateCryptoProviderBalanceOperationRequest) ApplyTo(doc *crypto_provider_balance_operation.CryptoProviderBalanceOperation) {
	updated := r.ToEntity()
	doc.ProviderAccountID, doc.TokenID, doc.ProviderObservationID, doc.SourceWalletID = updated.ProviderAccountID, updated.TokenID, updated.ProviderObservationID, updated.SourceWalletID
	doc.OperationType, doc.RecordType, doc.Amount = updated.OperationType, updated.RecordType, updated.Amount
	doc.TxHash, doc.ExternalRef, doc.Evidence, doc.Version = updated.TxHash, updated.ExternalRef, updated.Evidence, r.Version
}

func FromCryptoProviderBalanceOperation(doc *crypto_provider_balance_operation.CryptoProviderBalanceOperation) any {
	return doc
}

type CryptoProviderBalanceOperationDetail struct {
	*crypto_provider_balance_operation.CryptoProviderBalanceOperation
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

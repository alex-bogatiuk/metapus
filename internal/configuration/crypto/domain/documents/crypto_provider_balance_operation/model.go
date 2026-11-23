// Package crypto_provider_balance_operation provides audited provider prepayment entries.
package crypto_provider_balance_operation

import (
	"context"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/crypto_balance"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/crypto_provider_balance"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/posting"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

const (
	StatusPendingApproval = "pending_approval"
	StatusApproved        = "approved"
	StatusPosted          = "posted"
	StatusRejected        = "rejected"

	OperationTypeOpening    = "opening"
	OperationTypeFunding    = "funding"
	OperationTypeAdjustment = "adjustment"
	OperationTypeRefund     = "refund"
)

type CryptoProviderBalanceOperation struct {
	entity.Document
	ProviderAccountID     id.ID             `db:"provider_account_id" json:"providerAccountId" meta:"label:Аккаунт провайдера,ref:crypto_provider_account"`
	TokenID               id.ID             `db:"token_id" json:"tokenId" meta:"label:Токен,ref:token"`
	ProviderObservationID *id.ID            `db:"provider_observation_id" json:"providerObservationId,omitempty" meta:"label:Наблюдение баланса"`
	SourceWalletID        *id.ID            `db:"source_wallet_id" json:"sourceWalletId,omitempty" meta:"label:Кошелёк-источник,ref:wallet"`
	OperationType         string            `db:"operation_type" json:"operationType" meta:"label:Операция"`
	RecordType            entity.RecordType `db:"record_type" json:"recordType" meta:"label:Направление"`
	Amount                types.RawAmount   `db:"amount" json:"amount" meta:"label:Сумма"`
	TxHash                string            `db:"tx_hash" json:"txHash,omitempty" meta:"label:TX Hash"`
	ExternalRef           string            `db:"external_ref" json:"externalRef" meta:"label:Внешнее основание"`
	Evidence              []byte            `db:"evidence" json:"-"`
	Status                string            `db:"status" json:"status" meta:"label:Статус"`
	ApprovedBy            *id.ID            `db:"approved_by" json:"approvedBy,omitempty" meta:"label:Утвердил,ref:user"`
	ApprovedAt            *time.Time        `db:"approved_at" json:"approvedAt,omitempty" meta:"label:Дата одобрения"`
	ApprovalReason        string            `db:"approval_reason" json:"approvalReason,omitempty" meta:"label:Причина одобрения"`
	RejectedBy            *id.ID            `db:"rejected_by" json:"rejectedBy,omitempty" meta:"label:Отклонил,ref:user"`
	RejectedAt            *time.Time        `db:"rejected_at" json:"rejectedAt,omitempty" meta:"label:Дата отклонения"`
	RejectionReason       string            `db:"rejection_reason" json:"rejectionReason,omitempty" meta:"label:Причина отклонения"`
	DecisionMode          DecisionMode      `db:"decision_mode" json:"decisionMode,omitempty" meta:"label:Режим решения"`
	DecisionPolicyVersion int               `db:"decision_policy_version" json:"decisionPolicyVersion,omitzero"`
}

func (o *CryptoProviderBalanceOperation) Validate(ctx context.Context) error {
	if err := o.Document.Validate(ctx); err != nil {
		return err
	}
	if id.IsNil(o.CreatedBy) || strings.TrimSpace(o.Description) == "" {
		return apperror.NewValidation("provider balance operation requires an authenticated maker and reason")
	}
	if id.IsNil(o.ProviderAccountID) || id.IsNil(o.TokenID) {
		return apperror.NewValidation("provider account and token are required")
	}
	if !o.Amount.IsPositive() {
		return apperror.NewValidation("amount must be positive").WithDetail("field", "amount")
	}
	if o.ExternalRef == "" {
		return apperror.NewValidation("external reference is required").WithDetail("field", "externalRef")
	}
	switch o.OperationType {
	case OperationTypeOpening:
		if o.RecordType != entity.RecordTypeReceipt {
			return apperror.NewValidation("opening balance must be a receipt").WithDetail("field", "recordType")
		}
		if o.ProviderObservationID == nil || id.IsNil(*o.ProviderObservationID) || o.BasisID == nil || id.IsNil(*o.BasisID) || o.BasisType != "CryptoProviderBalanceIncident" {
			return apperror.NewValidation("opening balance requires a provider observation and reconciliation incident")
		}
		if o.SourceWalletID != nil || o.TxHash != "" {
			return apperror.NewValidation("opening balance cannot reference a funding wallet or transaction")
		}
	case OperationTypeFunding, OperationTypeRefund, OperationTypeAdjustment:
		// Client-supplied JSON booleans are not payment receipt evidence. Enable
		// these commands only with a server-verified, immutable receipt workflow.
		return apperror.NewValidation("a verified provider receipt workflow is required for this operation")
	default:
		return apperror.NewValidation("unsupported provider balance operation type").WithDetail("field", "operationType")
	}
	return nil
}

func (o *CryptoProviderBalanceOperation) CanPost(ctx context.Context) error {
	if err := o.Document.CanPost(ctx); err != nil {
		return err
	}
	if err := o.Validate(ctx); err != nil {
		return err
	}
	if o.Status != StatusApproved || o.Posted || o.PostedVersion != 0 {
		return apperror.NewValidation("provider balance operation must be approved before posting")
	}
	if o.ApprovedBy == nil || o.ApprovedAt == nil || strings.TrimSpace(o.ApprovalReason) == "" {
		return apperror.NewValidation("audited approval and reason are required")
	}
	return (DecisionAuthorization{ActorID: *o.ApprovedBy, Mode: o.DecisionMode, PolicyVersion: o.DecisionPolicyVersion}).validate(o.CreatedBy)
}

func (o *CryptoProviderBalanceOperation) CheckDecision(auth DecisionAuthorization, version int, reason string) error {
	if err := auth.validate(o.CreatedBy); err != nil {
		return err
	}
	if o.Version != version {
		return apperror.NewConcurrentModification(o.GetDocumentType(), o.ID)
	}
	if o.Status != StatusPendingApproval || o.Posted || o.PostedVersion != 0 || o.DecisionMode != "" || o.DecisionPolicyVersion != 0 {
		return apperror.NewValidation("provider balance operation is no longer pending at the requested version")
	}
	if strings.TrimSpace(reason) == "" || len(strings.TrimSpace(reason)) > 2000 {
		return apperror.NewValidation("decision reason is required")
	}
	return nil
}

func (o *CryptoProviderBalanceOperation) Approve(auth DecisionAuthorization, version int, reason string, now time.Time) error {
	if err := o.CheckDecision(auth, version, reason); err != nil {
		return err
	}
	actor := auth.ActorID
	o.DecisionMode, o.DecisionPolicyVersion = auth.Mode, auth.PolicyVersion
	o.Status, o.ApprovedBy, o.ApprovedAt, o.ApprovalReason = StatusApproved, &actor, &now, strings.TrimSpace(reason)
	o.UpdatedBy = actor
	return nil
}

func (o *CryptoProviderBalanceOperation) Reject(auth DecisionAuthorization, version int, reason string, now time.Time) error {
	if err := o.CheckDecision(auth, version, reason); err != nil {
		return err
	}
	actor := auth.ActorID
	o.DecisionMode, o.DecisionPolicyVersion = auth.Mode, auth.PolicyVersion
	o.Status, o.RejectedBy, o.RejectedAt, o.RejectionReason = StatusRejected, &actor, &now, strings.TrimSpace(reason)
	o.UpdatedBy = actor
	return nil
}

func (o *CryptoProviderBalanceOperation) MarkPosted() {
	o.Document.MarkPosted()
	o.Status = StatusPosted
}

func (*CryptoProviderBalanceOperation) CanModify() error {
	return apperror.NewForbidden("provider balance operations can only change through the audited decision workflow")
}

func (o *CryptoProviderBalanceOperation) GetDocumentType() string { return Definition.Name }

func (o *CryptoProviderBalanceOperation) GenerateCryptoProviderBalanceMovements(context.Context) ([]crypto_provider_balance.CryptoProviderBalanceMovement, error) {
	return []crypto_provider_balance.CryptoProviderBalanceMovement{crypto_provider_balance.NewCryptoProviderBalanceMovement(
		o.ID, o.GetDocumentType(), o.PostedVersion+1, o.Date, o.RecordType,
		o.ProviderAccountID, o.TokenID, o.OperationType, o.ExternalRef, o.Amount,
	)}, nil
}

func (o *CryptoProviderBalanceOperation) GenerateCryptoBalanceMovements(context.Context) ([]crypto_balance.CryptoBalanceMovement, error) {
	if o.OperationType != OperationTypeFunding || o.SourceWalletID == nil {
		return nil, nil
	}
	return []crypto_balance.CryptoBalanceMovement{crypto_balance.NewCryptoBalanceMovement(
		o.ID, o.GetDocumentType(), o.PostedVersion+1, o.Date, entity.RecordTypeExpense,
		*o.SourceWalletID, o.TokenID, o.Amount,
	)}, nil
}

var _ posting.Postable = (*CryptoProviderBalanceOperation)(nil)
var _ cryptoposting.CryptoProviderBalanceMovementSource = (*CryptoProviderBalanceOperation)(nil)
var _ cryptoposting.CryptoBalanceMovementSource = (*CryptoProviderBalanceOperation)(nil)

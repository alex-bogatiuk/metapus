package crypto_provider_balance_operation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestFundingRejectsClientReceiptAssertions(t *testing.T) {
	doc := &CryptoProviderBalanceOperation{Document: entity.NewDocument(), ProviderAccountID: id.New(), TokenID: id.New(), OperationType: "funding", RecordType: entity.RecordTypeReceipt, Amount: types.NewRawAmountFromInt64(1), TxHash: "tx", ExternalRef: "funding-1", Status: StatusApproved, Evidence: []byte(`{"chainReceiptVerified":true}`)}
	doc.CreatedBy = id.New()
	doc.Description = "funding reason"
	actor := id.New()
	now := time.Now()
	wallet := id.New()
	doc.ApprovedBy = &actor
	doc.ApprovedAt = &now
	doc.ApprovalReason = "checked"
	doc.SourceWalletID = &wallet
	require.ErrorContains(t, doc.CanPost(context.Background()), "verified provider receipt workflow")
	doc.Evidence = []byte(`{"chainReceiptVerified":true,"providerReceiptVerified":true}`)
	require.ErrorContains(t, doc.CanPost(context.Background()), "verified provider receipt workflow", "client JSON is not verified financial evidence")
}

func TestOpeningBalanceRequiresReconciliationEvidence(t *testing.T) {
	incidentID := id.New()
	observationID := id.New()
	doc := &CryptoProviderBalanceOperation{
		Document:              entity.NewDocument(),
		ProviderAccountID:     id.New(),
		TokenID:               id.New(),
		ProviderObservationID: &observationID,
		OperationType:         OperationTypeOpening,
		RecordType:            entity.RecordTypeReceipt,
		Amount:                types.NewRawAmountFromInt64(989_250_000),
		ExternalRef:           "opening-observation:" + observationID.String(),
		Status:                StatusPendingApproval,
	}
	doc.BasisType = "CryptoProviderBalanceIncident"
	doc.BasisID = &incidentID
	doc.CreatedBy = id.New()
	doc.Description = "provider opening evidence"
	require.NoError(t, doc.Validate(context.Background()))

	doc.ProviderObservationID = nil
	require.Error(t, doc.Validate(context.Background()))
}

func TestProviderOpeningDecisionAndGenericMutationGuards(t *testing.T) {
	doc := &CryptoProviderBalanceOperation{Document: entity.NewDocument(), Status: StatusPendingApproval}
	doc.CreatedBy = id.New()
	self := DecisionAuthorization{ActorID: doc.CreatedBy, Mode: DecisionIndependent, PolicyVersion: DecisionPolicyVersion}
	auth := DecisionAuthorization{ActorID: id.New(), Mode: DecisionIndependent, PolicyVersion: DecisionPolicyVersion}
	require.Error(t, doc.CheckDecision(self, 1, "self approval"))
	require.Error(t, doc.CheckDecision(auth, 2, "wrong version"))
	require.Error(t, doc.CheckDecision(auth, 1, ""))
	require.NoError(t, doc.Reject(auth, 1, "evidence is outdated", time.Now()))
	require.Error(t, doc.CheckDecision(auth, 1, "already rejected"))
	service := &Service{}
	require.Error(t, service.Create(context.Background(), doc))
	require.Error(t, service.Update(context.Background(), doc))
	require.Error(t, service.Delete(context.Background(), doc.ID))
	require.Error(t, service.SetDeletionMark(context.Background(), doc.ID, true))
	require.Error(t, service.Post(context.Background(), doc.ID))
	require.Error(t, service.Unpost(context.Background(), doc.ID))
	require.Error(t, service.PostAndSave(context.Background(), doc))
	require.Error(t, service.UpdateAndRepost(context.Background(), doc))
}

func TestOpeningBalanceRejectsExpenseDirection(t *testing.T) {
	incidentID := id.New()
	observationID := id.New()
	doc := &CryptoProviderBalanceOperation{
		Document:              entity.NewDocument(),
		ProviderAccountID:     id.New(),
		TokenID:               id.New(),
		ProviderObservationID: &observationID,
		OperationType:         OperationTypeOpening,
		RecordType:            entity.RecordTypeExpense,
		Amount:                types.NewRawAmountFromInt64(1),
		ExternalRef:           "opening-observation:" + observationID.String(),
	}
	doc.BasisType = "CryptoProviderBalanceIncident"
	doc.BasisID = &incidentID
	require.Error(t, doc.Validate(context.Background()))
}

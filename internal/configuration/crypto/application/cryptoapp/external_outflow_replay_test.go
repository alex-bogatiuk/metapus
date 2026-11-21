package cryptoapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
)

func TestExternalOutflowExactClassificationAndResolutionReplay(t *testing.T) {
	repo := newExternalOutflowRepoStub()
	service, holds := newExternalOutflowServiceForTest(&immediateTxManager{}, repo)
	maker, checker := id.New(), id.New()
	command := ClassifyExternalOutflowCommand{OutflowID: repo.outflow.ID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "confirmed loss", Evidence: map[string]any{"source": "audit", "review": true}}
	_, err := service.Classify(context.Background(), command, maker)
	require.NoError(t, err)
	_, err = service.Classify(context.Background(), command, maker)
	require.NoError(t, err)
	require.Equal(t, 1, repo.classifyCalls)
	require.Equal(t, 1, repo.adjustmentCreates)
	changed := command
	changed.Reason = "another reason"
	_, err = service.Classify(context.Background(), changed, maker)
	requireAppErrorCode(t, err, apperror.CodeConflict)
	_, err = service.Classify(context.Background(), command, id.New())
	requireAppErrorCode(t, err, apperror.CodeConflict)
	_, err = service.ApproveAdjustment(context.Background(), repo.outflow.Adjustment.ID, checker, "approved")
	require.NoError(t, err)
	_, err = service.ApproveAdjustment(context.Background(), repo.outflow.Adjustment.ID, checker, "approved")
	require.NoError(t, err)
	require.Equal(t, 1, repo.postingCalls)
	_, err = service.ApproveAdjustment(context.Background(), repo.outflow.Adjustment.ID, checker, "changed")
	requireAppErrorCode(t, err, apperror.CodeConflict)
	repo.freshReconcile = true
	_, err = service.Resolve(context.Background(), repo.outflow.ID, checker, "matched reconciliation")
	require.NoError(t, err)
	repo.freshReconcile = false
	_, err = service.Resolve(context.Background(), repo.outflow.ID, checker, "matched reconciliation")
	require.NoError(t, err, "a repeat returns the saved resolution, not a new reconciliation decision")
	_, err = service.Classify(context.Background(), command, maker)
	require.NoError(t, err, "the current classification remains a saved fact after resolution")
	require.Equal(t, 1, repo.resolveCalls)
	require.Equal(t, 1, holds.resolved)
	_, err = service.Resolve(context.Background(), repo.outflow.ID, id.New(), "matched reconciliation")
	requireAppErrorCode(t, err, apperror.CodeConflict)
	_, err = service.Resolve(context.Background(), repo.outflow.ID, checker, "changed")
	requireAppErrorCode(t, err, apperror.CodeConflict)
}

func TestExternalOutflowRejectReplayDoesNotBecomeApproval(t *testing.T) {
	repo := newExternalOutflowRepoStub()
	service, _ := newExternalOutflowServiceForTest(&immediateTxManager{}, repo)
	maker, checker := id.New(), id.New()
	_, err := service.Classify(context.Background(), ClassifyExternalOutflowCommand{OutflowID: repo.outflow.ID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "loss"}, maker)
	require.NoError(t, err)
	document := repo.outflow.Adjustment.ID
	_, err = service.RejectAdjustment(context.Background(), document, checker, "not proved")
	require.NoError(t, err)
	_, err = service.RejectAdjustment(context.Background(), document, checker, "not proved")
	require.NoError(t, err)
	_, err = service.RejectAdjustment(context.Background(), document, checker, "changed")
	requireAppErrorCode(t, err, apperror.CodeConflict)
	_, err = service.ApproveAdjustment(context.Background(), document, checker, "not proved")
	requireAppErrorCode(t, err, apperror.CodeConflict)
	require.Zero(t, repo.postingCalls)
}

package cryptoapp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
)

type unsignedIntentRepo struct {
	MoneyMovementApprovalRepository
}

func (unsignedIntentRepo) LoadBundle(context.Context, string, id.ID) (crypto.ApprovalBundle, bool, error) {
	return crypto.ApprovalBundle{}, false, nil
}

// Stand: approving TW-2026-00001 while it was pending_signature returned 500.
func TestApprovalBeforeIntentSigningIsARetryableConflict(t *testing.T) {
	service := &MoneyMovementApprovalService{repo: unsignedIntentRepo{}}
	_, _, _, err := service.loadAndVerify(t.Context(), "TreasuryWithdrawal", id.New())
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, apperror.CodeConflict, appErr.Code)
	require.ErrorContains(t, err, "not ready for approval")
}

package cryptoapp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
)

type failedApprovalIntent struct{ err error }

func (s failedApprovalIntent) BuildWithdrawalIntent(context.Context, string, id.ID) (crypto.MoneyMovementIntent, error) {
	return crypto.MoneyMovementIntent{}, s.err
}

func TestApprovalIntentErrorsKeepTechnicalAndBusinessCategories(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code string
	}{
		{errors.New("database connection lost"), apperror.CodeInternal},
		{apperror.NewNotFound("request", "id"), apperror.CodeConflict},
		{apperror.NewForbidden("scope denied"), apperror.CodeForbidden},
	} {
		service := &WithdrawalApprovalService{intents: failedApprovalIntent{tt.err}}
		err := service.Approve(t.Context(), id.New(), id.New())
		appErr, ok := apperror.AsAppError(err)
		require.True(t, ok)
		require.Equal(t, tt.code, appErr.Code)
	}
}

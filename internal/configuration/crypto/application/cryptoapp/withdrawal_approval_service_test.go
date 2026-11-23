package cryptoapp

import (
	"context"
	"errors"
	"testing"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/stretchr/testify/require"
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

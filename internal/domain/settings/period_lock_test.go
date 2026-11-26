package settings_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/domain/auth"
	"github.com/alex-bogatiuk/metapus/internal/domain/settings"
)

func TestPeriodLockClosesDaysUpToItsDate(t *testing.T) {
	t.Parallel()
	lock := settings.PeriodLockSettings{Date: "2026-10-31"}
	clerk := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: "clerk"})
	closed := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	open := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

	err := lock.CheckDocumentDates(clerk, "UTC", closed)
	appErr, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodePeriodClosed, appErr.Code)
	require.Contains(t, appErr.Message, "до 31.10.2026 включительно")
	require.NoError(t, lock.CheckDocumentDates(clerk, "UTC", open))
	require.Error(t, lock.CheckDocumentDates(clerk, "UTC", open, closed), "a stored date in the closed period is refused too")

	// 21:30 UTC on 31 October is already 1 November in Moscow.
	evening := time.Date(2026, 10, 31, 21, 30, 0, 0, time.UTC)
	require.Error(t, lock.CheckDocumentDates(clerk, "UTC", evening))
	require.NoError(t, lock.CheckDocumentDates(clerk, "Europe/Moscow", evening))

	accountant := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: "acc", Permissions: []string{auth.PermissionClosedPeriodEdit}})
	admin := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: "admin", IsAdmin: true})
	for name, ctx := range map[string]context.Context{"exception": accountant, "admin": admin, "system": context.Background()} {
		require.NoError(t, lock.CheckDocumentDates(ctx, "UTC", closed), name)
	}
	require.NoError(t, settings.PeriodLockSettings{}.CheckDocumentDates(clerk, "UTC", closed), "no date closes nothing")

	require.NoError(t, lock.Validate())
	require.Error(t, settings.PeriodLockSettings{Date: "31.10.2026"}.Validate())
}

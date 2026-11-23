package automation

import (
	"context"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/automations"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReportExecutorIsAssignedByAuthenticatedAuthor(t *testing.T) {
	caller, privileged := id.New(), id.New()
	config := &automations.ReportActionConfig{ExecutorUserID: privileged, DatasetKey: "document-journal", PeriodType: automations.PeriodAsOfNow}
	ctx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: caller.String()})
	require.NoError(t, BindReportExecutor(ctx, config))
	require.Equal(t, caller, config.ExecutorUserID)
	require.NoError(t, config.Validate())
	require.Error(t, BindReportExecutor(context.Background(), config))
	config.ExecutorUserID = id.Nil()
	require.Error(t, config.Validate(), "legacy rules require an explicit executor before execution")
}

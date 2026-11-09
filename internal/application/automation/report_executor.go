package automation

import (
	"context"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/domain/automations"
)

// BindReportExecutor assigns execution authority to the authenticated author
// when saving the report configuration. A client cannot nominate a privileged
// third party. The persisted identity is reauthorized at every execution.
func BindReportExecutor(ctx context.Context, config *automations.ReportActionConfig) error {
	if config == nil {
		return nil
	}
	user := appctx.GetUser(ctx)
	if user == nil {
		return apperror.NewUnauthorized("report author is required")
	}
	uid, err := id.Parse(user.UserID)
	if err != nil || id.IsNil(uid) {
		return apperror.NewForbidden("report author is unavailable")
	}
	config.ExecutorUserID = uid
	return nil
}

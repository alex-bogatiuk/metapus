package cryptohandlers

import (
	"context"
	"fmt"
	"time"

	"metapus/pkg/sdk/appctx"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/http/middleware"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tenant"
)

func requireObjectBoundMoneyMovementStepUp(
	ctx context.Context,
	checker middleware.StepUpChecker,
	actionCode string,
) (id.ID, id.ID, string, error) {
	if checker == nil {
		return id.Nil(), id.Nil(), "", apperror.NewInternal(fmt.Errorf("step-up checker is not configured"))
	}
	user := appctx.GetUser(ctx)
	if user == nil || user.UserID == "" {
		return id.Nil(), id.Nil(), "", apperror.NewUnauthorized("user context is required")
	}
	userID, err := id.Parse(user.UserID)
	if err != nil {
		return id.Nil(), id.Nil(), "", apperror.NewUnauthorized("invalid user identity").WithCause(err)
	}
	sessionID, err := id.Parse(user.SessionID)
	if err != nil || id.IsNil(sessionID) {
		return id.Nil(), id.Nil(), "", apperror.NewUnauthorized("invalid session")
	}
	tenantID := tenant.GetTenantID(ctx)
	if tenantID == "" {
		tenantID = appctx.GetTenantID(ctx)
	}
	ok, err := checker.HasFreshStepUp(ctx, sessionID, userID, tenantID, actionCode, time.Now().UTC())
	if err != nil {
		return id.Nil(), id.Nil(), "", apperror.NewInternal(err)
	}
	if !ok {
		return id.Nil(), id.Nil(), "", apperror.NewStepUpRequired(actionCode)
	}
	return userID, sessionID, tenantID, nil
}

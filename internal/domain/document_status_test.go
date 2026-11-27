package domain_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
	"github.com/alex-bogatiuk/metapus/internal/domain"
)

var orders = entity.Definition{
	Key: "order", Name: "Order", Route: "orders", Table: "doc_orders", Kind: entity.DocumentKind,
	Statuses: entity.StatusModel{
		Column: "status", Initial: "open",
		Values: []entity.StatusValue{{Value: "open", Label: "Открыт"}, {Value: "done", Label: "Выполнен"}},
		Transitions: []entity.StatusTransition{
			{From: []string{"open"}, To: "done"},
			{From: []string{"done"}, To: "open", Permission: "reopen"},
		},
	},
}

func TestCheckStatusChangeFollowsTheModel(t *testing.T) {
	t.Parallel()
	clerk := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: "clerk"})

	require.NoError(t, domain.CheckStatusChange(clerk, orders, "", "open"))
	require.NoError(t, domain.CheckStatusChange(clerk, orders, "open", "done"))
	require.NoError(t, domain.CheckStatusChange(clerk, orders, "done", "done"))

	var appErr *apperror.AppError
	err := domain.CheckStatusChange(clerk, orders, "open", "lost")
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, domain.CodeStatusUnknown, appErr.Code)

	err = domain.CheckStatusChange(clerk, orders, "done", "open")
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, apperror.CodeForbidden, appErr.Code, "reopening needs its permission")
	require.Contains(t, appErr.Message, "«Открыт»")
}

func TestCheckStatusChangeLetsHoldersOfThePermissionThrough(t *testing.T) {
	t.Parallel()
	manager := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: "manager", Permissions: []string{"document:order:reopen"}})
	admin := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: "admin", IsAdmin: true})
	require.Equal(t, "document:order:reopen", domain.StatusPermission(orders, "reopen"))

	require.NoError(t, domain.CheckStatusChange(manager, orders, "done", "open"))
	require.NoError(t, domain.CheckStatusChange(admin, orders, "done", "open"))
	require.NoError(t, domain.CheckStatusChange(context.Background(), orders, "done", "open"), "the system passes every declared transition")

	model := orders
	model.Statuses.Transitions = model.Statuses.Transitions[1:]
	var appErr *apperror.AppError
	err := domain.CheckStatusChange(admin, model, "open", "done")
	require.ErrorAs(t, err, &appErr, "no one makes an undeclared change")
	require.Equal(t, domain.CodeStatusTransitionNotAllowed, appErr.Code)
	require.Contains(t, appErr.Message, "из статуса «Открыт» в статус «Выполнен»")
}

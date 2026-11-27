package domain

import (
	"context"
	"fmt"
	"slices"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/entity"
)

// Error codes of the document status model.
const (
	// CodeStatusUnknown: the document is written with a status its model
	// does not declare.
	CodeStatusUnknown = "DOCUMENT_STATUS_UNKNOWN"
	// CodeStatusTransitionNotAllowed: no transition of the model allows the
	// change.
	CodeStatusTransitionNotAllowed = "DOCUMENT_STATUS_TRANSITION_NOT_ALLOWED"
)

// StatusPermission is the code of the permission a transition of def's
// status model needs: "<kind>:<key>:<action>".
func StatusPermission(def entity.Definition, action string) string {
	return def.Permission() + ":" + action
}

// CheckStatusChange refuses a write that changes a document of def from the
// status from (empty for a new document) to the status to, unless to is one
// of the model's statuses and a transition allows the change to the user.
// A transition with a permission is open to an administrator and to a user
// holding its permission; a request without a user is the system's and
// passes every transition the model declares.
func CheckStatusChange(ctx context.Context, def entity.Definition, from, to string) error {
	model := def.Statuses
	if !model.Known(to) {
		return apperror.NewBusinessRule(CodeStatusUnknown, fmt.Sprintf("Статус «%s» не предусмотрен для документа", to)).
			WithDetail("field", model.Column)
	}
	transitions, ok := model.TransitionsFor(from, to)
	start := from
	if start == "" {
		start = model.Initial
	}
	if !ok {
		return apperror.NewBusinessRule(CodeStatusTransitionNotAllowed,
			fmt.Sprintf("Нельзя перевести документ из статуса «%s» в статус «%s»", model.Label(start), model.Label(to))).
			WithDetail("field", model.Column).WithDetail("from", start).WithDetail("to", to)
	}
	user := appctx.GetUser(ctx)
	if len(transitions) == 0 || user == nil || user.IsAdmin {
		return nil
	}
	for _, t := range transitions {
		if t.Permission == "" || slices.Contains(user.Permissions, StatusPermission(def, t.Permission)) {
			return nil
		}
	}
	return apperror.NewForbidden(fmt.Sprintf("Нет права переводить документ в статус «%s»", model.Label(to))).
		WithDetail("field", model.Column).WithDetail("required_permission", StatusPermission(def, transitions[0].Permission))
}

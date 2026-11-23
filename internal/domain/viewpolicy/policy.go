// Package viewpolicy defines mutation rights for saved report and list configurations.
package viewpolicy

import (
	"context"

	"github.com/google/uuid"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	corectx "github.com/alex-bogatiuk/metapus/internal/core/context"
)

func UserID(ctx context.Context) (uuid.UUID, error) {
	user := corectx.GetUser(ctx)
	if user == nil {
		return uuid.Nil, apperror.NewUnauthorized("authentication required")
	}
	userID, err := uuid.Parse(user.UserID)
	if err != nil || userID == uuid.Nil {
		return uuid.Nil, apperror.NewUnauthorized("invalid user ID")
	}
	return userID, nil
}

func RequireCreate(ctx context.Context, visibility string) (uuid.UUID, error) {
	userID, err := UserID(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if visibility == "system" && !corectx.IsSystemAdmin(corectx.GetUser(ctx)) {
		return uuid.Nil, apperror.NewForbidden("only administrators may manage system configurations")
	}
	return userID, nil
}

func RequireEdit(ctx context.Context, visibility string, authorID *uuid.UUID) (uuid.UUID, error) {
	userID, err := UserID(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	owner := authorID != nil && *authorID == userID
	admin := corectx.IsSystemAdmin(corectx.GetUser(ctx))
	if (visibility == "personal" && owner) || (visibility == "shared" && (owner || admin)) || (visibility == "system" && admin) {
		return userID, nil
	}
	return uuid.Nil, apperror.NewForbidden("insufficient rights to modify configuration")
}

// Author preserves ownership when visibility changes. System configurations
// have no author; leaving system scope assigns the acting administrator.
func Author(visibility string, existing *uuid.UUID, userID uuid.UUID) *uuid.UUID {
	if visibility == "system" {
		return nil
	}
	if existing != nil {
		return existing
	}
	return &userID
}

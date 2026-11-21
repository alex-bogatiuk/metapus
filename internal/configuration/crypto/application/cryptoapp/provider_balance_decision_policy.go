package cryptoapp

import (
	"context"
	"errors"
	"slices"

	pbo "metapus/internal/configuration/crypto/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
)

type ProviderOpeningAdminAuthority struct {
	Basis       string
	AuthVersion int64
}

// LockSystemAdmin requires the active command transaction. ReadSystemAdmin is
// only for advisory read projections and never authorizes a persisted decision.
type ProviderOpeningAuthorityRepository interface {
	LockSystemAdmin(context.Context, id.ID) (ProviderOpeningAdminAuthority, error)
	ReadSystemAdmin(context.Context, id.ID) (ProviderOpeningAdminAuthority, error)
}

func providerDecisionPermission(user *appctx.UserContext, action string) bool {
	return user != nil && slices.Contains(user.Permissions, "document:crypto_provider_balance_operation:"+action)
}

func (s *ProviderBalanceWorkflowService) authorizeDecision(ctx context.Context, doc *pbo.CryptoProviderBalanceOperation, actor id.ID, action string) (pbo.DecisionAuthorization, ProviderOpeningAdminAuthority, error) {
	user := appctx.GetUser(ctx)
	admin := appctx.IsSystemAdmin(user)
	var authority ProviderOpeningAdminAuthority
	if actor == doc.CreatedBy && !id.IsNil(actor) && admin {
		var err error
		authority, err = s.authority.LockSystemAdmin(ctx, actor)
		if err != nil {
			return pbo.DecisionAuthorization{}, authority, apperror.NewInternal(err)
		}
		admin = authority.Basis != ""
	}
	authorization, err := pbo.AuthorizeDecision(doc.CreatedBy, actor, admin, providerDecisionPermission(user, action))
	return authorization, authority, err
}

func (s *ProviderBalanceWorkflowService) ActionAvailability(ctx context.Context, doc *pbo.CryptoProviderBalanceOperation) (map[string]entity.ActionAvailability, error) {
	user := appctx.GetUser(ctx)
	actor := id.Nil()
	if user != nil {
		actor, _ = id.Parse(user.UserID)
	}
	admin := appctx.IsSystemAdmin(user)
	if admin && actor == doc.CreatedBy && !id.IsNil(actor) && doc.Status == pbo.StatusPendingApproval {
		authority, err := s.authority.ReadSystemAdmin(ctx, actor)
		if err != nil {
			return nil, apperror.NewInternal(err)
		}
		admin = authority.Basis != ""
	}
	result := make(map[string]entity.ActionAvailability, 2)
	for _, action := range []string{"approve", "reject"} {
		state := entity.ActionAvailability{}
		switch {
		case security.GetDataScope(ctx).CanMutate() != nil:
			state.ReasonCode = "READ_ONLY_SCOPE"
		case doc.Status != pbo.StatusPendingApproval || doc.Posted:
			state.ReasonCode = "PROVIDER_OPENING_NOT_PENDING"
		default:
			authorization, err := pbo.AuthorizeDecision(doc.CreatedBy, actor, admin, providerDecisionPermission(user, action))
			if err == nil {
				state.Allowed, state.DecisionMode = true, string(authorization.Mode)
			} else {
				var appErr *apperror.AppError
				if errors.As(err, &appErr) {
					state.ReasonCode, _ = appErr.Details["reasonCode"].(string)
				}
			}
		}
		result[action] = state
	}
	return result, nil
}

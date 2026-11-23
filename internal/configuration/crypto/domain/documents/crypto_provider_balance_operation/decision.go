package crypto_provider_balance_operation

import (
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type DecisionMode string

const (
	DecisionIndependent         DecisionMode = "independent"
	DecisionAdminSelf           DecisionMode = "admin_self"
	DecisionPolicyVersion                    = 1
	ReasonSelfDecisionForbidden              = "PROVIDER_OPENING_SELF_DECISION_FORBIDDEN"
	ReasonPermissionRequired                 = "PROVIDER_OPENING_PERMISSION_REQUIRED"
	ReasonInvalidMaker                       = "PROVIDER_OPENING_INVALID_MAKER"
)

// DecisionAuthorization is a domain value, never a client DTO. The application
// supplies capabilities established by its trusted authorization adapters.
type DecisionAuthorization struct {
	ActorID       id.ID
	Mode          DecisionMode
	PolicyVersion int
}

func AuthorizeDecision(maker, actor id.ID, systemAdmin, permitted bool) (DecisionAuthorization, error) {
	if id.IsNil(maker) || id.IsNil(actor) {
		return DecisionAuthorization{}, apperror.NewForbidden("an authenticated maker and decision actor are required").WithDetail("reasonCode", ReasonInvalidMaker)
	}
	if maker == actor && !systemAdmin {
		return DecisionAuthorization{}, apperror.NewForbidden("another operator must decide on this opening").WithDetail("reasonCode", ReasonSelfDecisionForbidden)
	}
	if !systemAdmin && !permitted {
		return DecisionAuthorization{}, apperror.NewForbidden("provider opening decision permission is required").WithDetail("reasonCode", ReasonPermissionRequired)
	}
	mode := DecisionIndependent
	if maker == actor {
		mode = DecisionAdminSelf
	}
	return DecisionAuthorization{ActorID: actor, Mode: mode, PolicyVersion: DecisionPolicyVersion}, nil
}

func (a DecisionAuthorization) validate(maker id.ID) error {
	if id.IsNil(maker) || id.IsNil(a.ActorID) || a.PolicyVersion != DecisionPolicyVersion ||
		(a.Mode != DecisionIndependent && a.Mode != DecisionAdminSelf) ||
		(a.Mode == DecisionIndependent && maker == a.ActorID) ||
		(a.Mode == DecisionAdminSelf && maker != a.ActorID) {
		return apperror.NewForbidden("invalid provider opening decision authorization")
	}
	return nil
}

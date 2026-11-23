package cryptoapp

import (
	"context"
	"strings"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

type OperationalHoldScope string

const (
	OperationalHoldScopeNetwork OperationalHoldScope = "network"
	OperationalHoldScopeWallet  OperationalHoldScope = "wallet"
)

// AcquireOperationalHoldCommand identifies one independent operational stop.
// OwnerType+OwnerID is the immutable recovery boundary: only that owner's
// workflow is allowed to resolve the hold.
type AcquireOperationalHoldCommand struct {
	Scope     OperationalHoldScope
	ScopeID   id.ID
	Reason    string
	OwnerType string
	OwnerID   string
	Details   map[string]any
	HeldBy    string
}

type OperationalHoldRepository interface {
	AcquireOperationalHold(context.Context, AcquireOperationalHoldCommand) error
	ResolveOperationalHolds(context.Context, string, string, string, string) (int64, error)
}

// OperationalHoldService is the sole application-level owner of hold
// acquisition and resolution. It intentionally exposes no release-by-scope
// operation: resolving one incident must never clear another incident's hold.
type OperationalHoldService struct {
	txm  tx.Manager
	repo OperationalHoldRepository
}

func NewOperationalHoldService(txm tx.Manager, repo OperationalHoldRepository) *OperationalHoldService {
	if txm == nil || repo == nil {
		panic("OperationalHoldService requires tx manager and repository")
	}
	return &OperationalHoldService{txm: txm, repo: repo}
}

func (s *OperationalHoldService) Acquire(ctx context.Context, command AcquireOperationalHoldCommand) error {
	command.Reason = strings.TrimSpace(command.Reason)
	command.OwnerType = strings.TrimSpace(command.OwnerType)
	command.OwnerID = strings.TrimSpace(command.OwnerID)
	command.HeldBy = strings.TrimSpace(command.HeldBy)
	if (command.Scope != OperationalHoldScopeNetwork && command.Scope != OperationalHoldScopeWallet) ||
		id.IsNil(command.ScopeID) || command.Reason == "" || command.OwnerType == "" ||
		command.OwnerID == "" || command.HeldBy == "" {
		return apperror.NewValidation("hold scope, reason, owner and actor are required")
	}
	if command.Details == nil {
		command.Details = map[string]any{}
	}
	return s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.AcquireOperationalHold(txCtx, command)
	})
}

// ResolveOwner resolves only holds acquired by the supplied owner workflow.
// Repeating the same command after resolution is a safe no-op in the adapter.
func (s *OperationalHoldService) ResolveOwner(ctx context.Context, ownerType, ownerID, actor, reason string) error {
	ownerType = strings.TrimSpace(ownerType)
	ownerID = strings.TrimSpace(ownerID)
	actor = strings.TrimSpace(actor)
	reason = strings.TrimSpace(reason)
	if ownerType == "" || ownerID == "" || actor == "" || reason == "" {
		return apperror.NewValidation("hold owner, resolver and reason are required")
	}
	return s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		_, err := s.repo.ResolveOperationalHolds(txCtx, ownerType, ownerID, actor, reason)
		return err
	})
}

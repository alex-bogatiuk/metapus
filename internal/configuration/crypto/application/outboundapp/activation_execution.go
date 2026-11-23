package outboundapp

import (
	"context"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tx"
)

// Run accepts only local DB work. Signer/RPC calls must run after it commits.
type SigningExecutionGuard interface {
	Run(context.Context, func(context.Context) error) error
	BeginDispatch(context.Context, crypto.SigningAuthorization, string, func(context.Context) error) error
}
type GuardedAuthorizations interface {
	CreateGuarded(context.Context, SigningAttemptInput, SigningExecutionGuard) (SigningAuthorizationRecord, error)
	SignOrReuseGuarded(context.Context, crypto.PolicyAwareTransactionSigner, SigningAuthorizationRecord, SigningExecutionGuard) (crypto.SignedTxEnvelope, error)
	CheckCustodyDispatchGuarded(context.Context, crypto.SigningAuthorization, *crypto.FamilySignerRegistry, SigningExecutionGuard) error
}

type ActivationClaimAction string

const (
	ActivationClaimHeld             ActivationClaimAction = "held"
	ActivationClaimFailed           ActivationClaimAction = "failed"
	ActivationClaimSigning          ActivationClaimAction = "signing"
	ActivationClaimExternallyActive ActivationClaimAction = "externally_active"
	ActivationClaimDeferred         ActivationClaimAction = "deferred"
)

type ActivationClaimUpdate struct {
	Action ActivationClaimAction
	Reason string
	Delay  time.Duration
}
type ActivationBroadcastEvidence struct {
	AuthorizationID string
	ReservationID   id.ID
	Fingerprint     string
	TxHash          string
}

type activationExecutionGuard struct {
	repo         ActivationRepository
	transactions tx.Manager
	task         AccountActivationTask
}

func (g activationExecutionGuard) Run(ctx context.Context, work func(context.Context) error) error {
	if g.repo == nil || g.transactions == nil {
		return apperror.NewValidation("activation execution guard is not configured")
	}
	return g.transactions.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := g.repo.PrepareAccountActivationState(ctx, g.task); err != nil {
			return err
		}
		if work != nil {
			return work(ctx)
		}
		return nil
	})
}

// BeginDispatch admits one external phase. The custody dispatch permit
// commits its own transaction and refuses a caller transaction, so it runs
// between two fenced claim checks. The dispatch marker commits last, right
// before the caller's signer/RPC call: a permit left by a lost lease is only
// a permit, never signing exposure.
func (g activationExecutionGuard) BeginDispatch(ctx context.Context, auth crypto.SigningAuthorization, phase string, work func(context.Context) error) error {
	if err := g.Run(ctx, nil); err != nil {
		return err
	}
	if work != nil {
		if err := work(ctx); err != nil {
			return err
		}
	}
	return g.Run(ctx, func(ctx context.Context) error {
		return g.repo.BeginAccountActivationDispatch(ctx, g.task, auth, phase)
	})
}
func (e *AccountActivationExecutor) executionGuard(task AccountActivationTask) SigningExecutionGuard {
	return activationExecutionGuard{repo: e.outboundRepo, transactions: e.transactions, task: task}
}
func (e *AccountActivationExecutor) updateClaim(ctx context.Context, task AccountActivationTask, action ActivationClaimAction, reason string, delay time.Duration) error {
	return e.outboundRepo.ApplyAccountActivationClaim(ctx, task, ActivationClaimUpdate{Action: action, Reason: reason, Delay: delay})
}
func runSigningCommand(ctx context.Context, guard SigningExecutionGuard, work func(context.Context) error) error {
	if guard == nil {
		return work(ctx)
	}
	return guard.Run(ctx, work)
}
func runSigningDispatch(ctx context.Context, guard SigningExecutionGuard, auth crypto.SigningAuthorization, phase string, work func(context.Context) error) error {
	if guard == nil {
		return work(ctx)
	}
	return guard.BeginDispatch(ctx, auth, phase, work)
}
func requireActivationSigningGuard(purpose crypto.SigningPurpose, guard SigningExecutionGuard) error {
	if purpose == crypto.SigningPurposeActivation && guard == nil {
		return apperror.NewConflict("activation signing dispatch requires an execution claim")
	}
	return nil
}

package cryptoapp

import (
	"context"

	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/crypto"
)

const balanceIncidentOwner = "reconciliation_incident"
const balanceReconciliationActor = "reconciliation-worker"

type BalanceReconciliationRepository interface {
	LockBalanceWallet(context.Context, id.ID) error
	InsertBalanceCheck(context.Context, crypto.BalanceObservation) (bool, error)
	FindOpenBalanceIncident(context.Context, crypto.BalanceObservation) (id.ID, error)
	CreateBalanceIncident(context.Context, crypto.BalanceObservation) (id.ID, error)
	UpdateBalanceIncident(context.Context, id.ID, crypto.BalanceObservation, bool) error
	AppendBalanceObservation(context.Context, id.ID, crypto.BalanceObservation) error
	InitialBalanceIncidentObservation(context.Context, id.ID) (crypto.BalanceObservation, error)
	HasBalanceIncidentHold(context.Context, id.ID) (bool, error)
	PendingWalletCheckReady(context.Context, id.ID) (bool, error)
}

type BalanceReconciliationOutcome struct {
	Created, Updated, Resolved bool
}

// BalanceReconciliationService commits evidence, the incident projection and
// its owned hold together. Chain reads happen before this use case; no money
// movements or balance corrections are performed here.
type BalanceReconciliationService struct {
	txm   tx.Manager
	repo  BalanceReconciliationRepository
	holds *OperationalHoldService
}

func NewBalanceReconciliationService(txm tx.Manager, repo BalanceReconciliationRepository, holds *OperationalHoldService) *BalanceReconciliationService {
	if txm == nil || repo == nil || holds == nil {
		panic("balance reconciliation requires transactional repositories and holds")
	}
	return &BalanceReconciliationService{txm: txm, repo: repo, holds: holds}
}

func (s *BalanceReconciliationService) Record(ctx context.Context, o crypto.BalanceObservation) (BalanceReconciliationOutcome, error) {
	var result BalanceReconciliationOutcome
	if err := o.Validate(); err != nil {
		return result, err
	}
	err := s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		// Serializes absent/open/resolved transitions and allocation readiness for
		// this wallet. In particular a retry cannot recreate a resolved incident.
		if err := s.repo.LockBalanceWallet(ctx, o.WalletID); err != nil {
			return err
		}
		inserted, err := s.repo.InsertBalanceCheck(ctx, o)
		if err != nil || !inserted {
			return err
		}
		incidentID, err := s.repo.FindOpenBalanceIncident(ctx, o)
		if err != nil {
			return err
		}
		matched := o.Delta().IsZero()
		if id.IsNil(incidentID) {
			if matched {
				return nil
			}
			incidentID, err = s.repo.CreateBalanceIncident(ctx, o)
			if err != nil {
				return err
			}
			result.Created = true
		} else {
			if err := s.repo.UpdateBalanceIncident(ctx, incidentID, o, matched); err != nil {
				return err
			}
			result.Resolved, result.Updated = matched, !matched
		}
		if err := s.repo.AppendBalanceObservation(ctx, incidentID, o); err != nil {
			return err
		}
		if !matched {
			initial, err := s.repo.InitialBalanceIncidentObservation(ctx, incidentID)
			if err != nil {
				return err
			}
			return s.holds.Acquire(ctx, AcquireOperationalHoldCommand{
				Scope: OperationalHoldScopeWallet, ScopeID: o.WalletID,
				Reason: "chain_balance_mismatch", OwnerType: balanceIncidentOwner, OwnerID: incidentID.String(),
				HeldBy: balanceReconciliationActor,
				Details: map[string]any{"firstRunId": initial.RunID.String(), "kind": initial.Kind,
					"tokenId": initial.TokenID.String(), "deltaAmount": initial.Delta().String()},
			})
		}
		// Non-pool wallets have incidents but do not acquire allocation holds.
		held, err := s.repo.HasBalanceIncidentHold(ctx, incidentID)
		if err != nil || !held {
			return err
		}
		return s.holds.ResolveOwner(ctx, balanceIncidentOwner, incidentID.String(), balanceReconciliationActor, "auto_reconciled")
	})
	if err != nil {
		return BalanceReconciliationOutcome{}, err
	}
	return result, nil
}

// CompleteWalletCheck is called only after all token reads succeeded without
// delta. It releases the provisioning workflow's hold, never incident holds.
func (s *BalanceReconciliationService) CompleteWalletCheck(ctx context.Context, walletID id.ID) error {
	return s.txm.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LockBalanceWallet(ctx, walletID); err != nil {
			return err
		}
		ready, err := s.repo.PendingWalletCheckReady(ctx, walletID)
		if err != nil || !ready {
			return err
		}
		return s.holds.ResolveOwner(ctx, "wallet_reconciliation", walletID.String(), balanceReconciliationActor, "chain_balance_verified")
	})
}

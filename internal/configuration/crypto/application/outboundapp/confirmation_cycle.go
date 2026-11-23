package outboundapp

import (
	"context"
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/logger"
)

const (
	_replacementAge   = 2 * time.Minute
	_replacementLease = 90 * time.Second
)

type ConfirmationClaims interface {
	ClaimOutboundSettlements(context.Context, string, int) ([]SettlementTask, error)
	ClaimStuckReplacementAuthorizations(context.Context, string, int, time.Duration, time.Duration) ([]ReplacementCandidate, error)
	ReleaseReplacementClaim(context.Context, string, string) error
}
type FundingConfirmationBatch interface {
	RunBatch(context.Context, string, int) error
}
type ReplacementExecutor interface {
	Execute(context.Context, ReplacementCandidate) error
}
type SettlementExecutor interface {
	Settle(context.Context, SettlementTask) error
	RecoverOrphanedResources(context.Context, int) error
}
type ResourcePreparerCatalog interface {
	All() []ResourcePreparer
}

type ConfirmationCycleConfig struct {
	Claims       ConfirmationClaims
	Funding      FundingConfirmationBatch
	Replacements ReplacementExecutor
	Settlement   SettlementExecutor
	Resources    ResourcePreparerCatalog
	Logger       *logger.Logger
}
type ConfirmationCycle struct {
	claims       ConfirmationClaims
	funding      FundingConfirmationBatch
	replacements ReplacementExecutor
	settlement   SettlementExecutor
	resources    ResourcePreparerCatalog
	log          *logger.Logger
}

func NewConfirmationCycle(c ConfirmationCycleConfig) *ConfirmationCycle {
	if c.Logger == nil {
		c.Logger = logger.Default()
	}
	return &ConfirmationCycle{claims: c.Claims, funding: c.Funding, replacements: c.Replacements, settlement: c.Settlement, resources: c.Resources, log: c.Logger}
}

func (p *ConfirmationCycle) RunBatch(ctx context.Context, workerID string, limit int) error {
	if p == nil || p.claims == nil || p.funding == nil || p.settlement == nil {
		return fmt.Errorf("outbound confirmation cycle is not configured")
	}
	owner := workerID + "/native-funding/" + id.New().String()
	if err := p.funding.RunBatch(ctx, owner, limit); err != nil {
		p.log.Errorw("native funding confirmation failed", "error", err)
	}
	// Rebind a previously mined generation before claiming settlement tasks.
	// The task must carry the winning hash into every financial effect.
	if err := p.replaceStuckTransactions(ctx, workerID, limit); err != nil {
		p.log.Errorw("stuck transaction replacement failed", "error", err)
	}
	if p.resources != nil {
		for _, preparer := range p.resources.All() {
			if err := preparer.MaintainResources(ctx, workerID); err != nil {
				p.log.Errorw("resource rental maintenance failed", "error", err)
			}
		}
	}
	if err := p.settlement.RecoverOrphanedResources(ctx, limit); err != nil {
		p.log.Errorw("orphaned resource allocation recovery failed", "error", err)
	}
	tasks, err := p.claims.ClaimOutboundSettlements(ctx, workerID, limit)
	if err != nil {
		return err
	}
	var firstErr error
	for _, task := range tasks {
		if err := p.settlement.Settle(ctx, task); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			p.log.Errorw("outbound transaction settlement failed",
				"kind", task.Kind, "document_id", task.DocumentID, "line_id", task.LineID, "tx_hash", task.TxHash, "error", err)
		}
	}
	return firstErr
}

func (p *ConfirmationCycle) replaceStuckTransactions(ctx context.Context, workerID string, limit int) error {
	if p.replacements == nil {
		return nil
	}
	candidates, err := p.claims.ClaimStuckReplacementAuthorizations(ctx, workerID, limit, _replacementAge, _replacementLease)
	if err != nil {
		return err
	}
	var firstErr error
	for _, candidate := range candidates {
		if err := p.replaceCandidate(ctx, workerID, candidate); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			p.log.Errorw("replacement assessment failed", "authorization_id", candidate.AuthorizationID,
				"basis_type", candidate.BasisType, "basis_id", candidate.BasisID, "attempt", candidate.Attempt, "generation", candidate.Generation, "error", err)
		}
	}
	return firstErr
}

func (p *ConfirmationCycle) replaceCandidate(ctx context.Context, workerID string, candidate ReplacementCandidate) error {
	defer func() {
		if err := p.claims.ReleaseReplacementClaim(ctx, candidate.AuthorizationID, workerID); err != nil {
			p.log.Errorw("failed to release replacement claim", "authorization_id", candidate.AuthorizationID, "error", err)
		}
	}()
	return p.replacements.Execute(ctx, candidate)
}

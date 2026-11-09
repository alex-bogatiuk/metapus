package cryptoapp

import (
	"context"
	"fmt"
	"strings"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/tx"
	"metapus/internal/domain/crypto"
)

type ExternalOutflowRepository interface {
	ListExternalOutflows(context.Context, string, int) ([]crypto.ExternalOutflow, error)
	GetExternalOutflow(context.Context, id.ID, bool) (*crypto.ExternalOutflow, error)
	ReplayClassification(context.Context, *crypto.ExternalOutflow, ClassifyExternalOutflowCommand, id.ID) (bool, error)
	ReplayResolution(context.Context, *crypto.ExternalOutflow, id.ID, string) (bool, error)
	ReplayCustodyDecision(context.Context, id.ID, id.ID, string, string) (*crypto.CryptoCustodyAdjustment, error)
	MatchExactBasis(context.Context, *crypto.ExternalOutflow, string, id.ID) (bool, error)
	HasFalsePositiveProof(context.Context, *crypto.ExternalOutflow) (bool, error)
	ClassifyExternalOutflow(context.Context, *crypto.ExternalOutflow, ClassifyExternalOutflowCommand, id.ID) error
	CreateCustodyAdjustment(context.Context, *crypto.ExternalOutflow, id.ID) (*crypto.CryptoCustodyAdjustment, error)
	HasFreshSuccessfulReconciliation(context.Context, *crypto.ExternalOutflow) (bool, error)
	ResolveExternalOutflow(context.Context, *crypto.ExternalOutflow, id.ID, string) error
	PrepareCustodyAdjustmentForPosting(context.Context, id.ID, id.ID) (*crypto.CryptoCustodyAdjustment, error)
	MarkCustodyAdjustmentPosted(context.Context, *crypto.CryptoCustodyAdjustment, id.ID, string) error
	RejectCustodyAdjustment(context.Context, id.ID, id.ID, string) (*crypto.CryptoCustodyAdjustment, error)
}

type ExternalOutflowService struct {
	txm     tx.Manager
	repo    ExternalOutflowRepository
	holds   *OperationalHoldService
	posting FinancialPostingEngine
}

func NewExternalOutflowService(txm tx.Manager, repo ExternalOutflowRepository, holds *OperationalHoldService, posting FinancialPostingEngine) *ExternalOutflowService {
	if txm == nil || repo == nil || holds == nil || posting == nil {
		panic("ExternalOutflowService requires tx manager, repository, operational hold service and posting engine")
	}
	return &ExternalOutflowService{txm: txm, repo: repo, holds: holds, posting: posting}
}

type ClassifyExternalOutflowCommand struct {
	OutflowID        id.ID
	Classification   crypto.ExternalOutflowClassification
	MatchedBasisType string
	MatchedBasisID   *id.ID
	Reason           string
	Evidence         map[string]any
}

type ExternalOutflowResolutionReadiness struct {
	CanResolve bool
	Blockers   []string
}

func hasFinalCustodyLossEffect(outflow *crypto.ExternalOutflow) bool {
	if outflow == nil || (outflow.FinalityStatus != "confirmed" && outflow.FinalityStatus != "finalized") {
		return false
	}
	return outflow.RequiredConfirmations <= 0 || outflow.Confirmations >= outflow.RequiredConfirmations
}

func (s *ExternalOutflowService) List(ctx context.Context, status string, limit int) ([]crypto.ExternalOutflow, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	return s.repo.ListExternalOutflows(ctx, status, limit)
}

func (s *ExternalOutflowService) Get(ctx context.Context, outflowID id.ID) (*crypto.ExternalOutflow, error) {
	return s.repo.GetExternalOutflow(ctx, outflowID, false)
}

func (s *ExternalOutflowService) ResolutionReadiness(ctx context.Context, outflow *crypto.ExternalOutflow, actorID id.ID) (ExternalOutflowResolutionReadiness, error) {
	result := ExternalOutflowResolutionReadiness{}
	if outflow.Status == crypto.ExternalOutflowResolved {
		result.Blockers = append(result.Blockers, "external outflow is already resolved")
		return result, nil
	}
	if outflow.Status != crypto.ExternalOutflowClassified {
		result.Blockers = append(result.Blockers, "classification is required")
		return result, nil
	}
	if outflow.ClassifiedBy != nil && *outflow.ClassifiedBy == actorID {
		result.Blockers = append(result.Blockers, "classifier cannot resolve the same external outflow")
	}
	if outflow.Classification == crypto.OutflowUnauthorizedLoss && (outflow.Adjustment == nil || outflow.Adjustment.Status != "posted") {
		result.Blockers = append(result.Blockers, "custody adjustment must be approved and posted")
	}
	if outflow.Classification == crypto.OutflowUnauthorizedLoss && !hasFinalCustodyLossEffect(outflow) {
		result.Blockers = append(result.Blockers, "custody loss chain effect is no longer final")
	}
	fresh, err := s.repo.HasFreshSuccessfulReconciliation(ctx, outflow)
	if err != nil {
		return result, err
	}
	if !fresh {
		result.Blockers = append(result.Blockers, "fresh successful reconciliation after classification is required")
	}
	result.CanResolve = len(result.Blockers) == 0
	return result, nil
}

func (s *ExternalOutflowService) Classify(ctx context.Context, command ClassifyExternalOutflowCommand, actorID id.ID) (*crypto.ExternalOutflow, error) {
	if id.IsNil(command.OutflowID) || id.IsNil(actorID) || !command.Classification.Valid() || strings.TrimSpace(command.Reason) == "" {
		return nil, apperror.NewValidation("outflow, classification, actor and reason are required")
	}
	if command.Classification != crypto.OutflowMatchedBasis && (command.MatchedBasisType != "" || command.MatchedBasisID != nil) {
		return nil, apperror.NewValidation("only matched-basis classification may specify a basis")
	}
	var result *crypto.ExternalOutflow
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		outflow, err := s.repo.GetExternalOutflow(txCtx, command.OutflowID, true)
		if err != nil {
			return err
		}
		replayed, err := s.repo.ReplayClassification(txCtx, outflow, command, actorID)
		if err != nil {
			return err
		}
		if replayed {
			result, err = s.repo.GetExternalOutflow(txCtx, command.OutflowID, false)
			return err
		}
		reclassifying := outflow.Status == crypto.ExternalOutflowClassified && outflow.Classification == crypto.OutflowUnauthorizedLoss && outflow.Adjustment != nil && outflow.Adjustment.Status == "rejected"
		if outflow.Status != crypto.ExternalOutflowPendingReview && !reclassifying {
			return apperror.NewConflict("external outflow is not available for classification")
		}
		if reclassifying && command.Classification == crypto.OutflowUnauthorizedLoss {
			return apperror.NewValidation("a rejected custody adjustment can only be reclassified to matched basis or false positive/reorg")
		}
		switch command.Classification {
		case crypto.OutflowMatchedBasis:
			if command.MatchedBasisID == nil || id.IsNil(*command.MatchedBasisID) || strings.TrimSpace(command.MatchedBasisType) == "" {
				return apperror.NewValidation("matched basis identity is required")
			}
			matches, err := s.repo.MatchExactBasis(txCtx, outflow, command.MatchedBasisType, *command.MatchedBasisID)
			if err != nil {
				return err
			}
			if !matches {
				return apperror.NewValidation("basis does not prove the exact observed chain effect")
			}
		case crypto.OutflowFalsePositiveOrReorg:
			proved, err := s.repo.HasFalsePositiveProof(txCtx, outflow)
			if err != nil {
				return err
			}
			if !proved {
				return apperror.NewValidation("removed or reorg chain proof is required")
			}
		case crypto.OutflowUnauthorizedLoss:
			if outflow.TokenID == nil || outflow.SourceWalletID == nil {
				return apperror.NewValidation("custody adjustment requires a resolved wallet and token")
			}
			if !hasFinalCustodyLossEffect(outflow) {
				return apperror.NewValidation("custody loss classification requires a final chain effect with all confirmations")
			}
		}
		if err := s.repo.ClassifyExternalOutflow(txCtx, outflow, command, actorID); err != nil {
			return err
		}
		if command.Classification == crypto.OutflowUnauthorizedLoss && !reclassifying {
			if _, err := s.repo.CreateCustodyAdjustment(txCtx, outflow, actorID); err != nil {
				return err
			}
		}
		result, err = s.repo.GetExternalOutflow(txCtx, command.OutflowID, false)
		return err
	})
	return result, err
}

func (s *ExternalOutflowService) Resolve(ctx context.Context, outflowID, actorID id.ID, reason string) (*crypto.ExternalOutflow, error) {
	if id.IsNil(outflowID) || id.IsNil(actorID) || strings.TrimSpace(reason) == "" {
		return nil, apperror.NewValidation("outflow, resolver and reason are required")
	}
	var result *crypto.ExternalOutflow
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		outflow, err := s.repo.GetExternalOutflow(txCtx, outflowID, true)
		if err != nil {
			return err
		}
		replayed, err := s.repo.ReplayResolution(txCtx, outflow, actorID, reason)
		if err != nil {
			return err
		}
		if replayed {
			result, err = s.repo.GetExternalOutflow(txCtx, outflowID, false)
			return err
		}
		if outflow.Status != crypto.ExternalOutflowClassified || outflow.ClassifiedBy == nil {
			return apperror.NewConflict("external outflow is not classified")
		}
		if *outflow.ClassifiedBy == actorID {
			return apperror.NewForbidden("classifier cannot resolve the same external outflow")
		}
		if outflow.Classification == crypto.OutflowUnauthorizedLoss && (outflow.Adjustment == nil || outflow.Adjustment.Status != "posted") {
			return apperror.NewConflict("custody adjustment must be approved and posted first")
		}
		if outflow.Classification == crypto.OutflowUnauthorizedLoss && !hasFinalCustodyLossEffect(outflow) {
			return apperror.NewConflict("custody loss chain effect is no longer final")
		}
		fresh, err := s.repo.HasFreshSuccessfulReconciliation(txCtx, outflow)
		if err != nil {
			return err
		}
		if !fresh {
			return apperror.NewConflict("a fresh successful reconciliation after classification is required")
		}
		if err := s.repo.ResolveExternalOutflow(txCtx, outflow, actorID, reason); err != nil {
			return err
		}
		if err := s.holds.ResolveOwner(txCtx, "external_outflow", outflow.ID.String(), actorID.String(), reason); err != nil {
			return err
		}
		result, err = s.repo.GetExternalOutflow(txCtx, outflowID, false)
		return err
	})
	return result, err
}

func (s *ExternalOutflowService) ApproveAdjustment(ctx context.Context, adjustmentID, actorID id.ID, reason string) (*crypto.CryptoCustodyAdjustment, error) {
	if id.IsNil(adjustmentID) || id.IsNil(actorID) || strings.TrimSpace(reason) == "" {
		return nil, apperror.NewValidation("adjustment, checker and reason are required")
	}
	var result *crypto.CryptoCustodyAdjustment
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		replayed, err := s.repo.ReplayCustodyDecision(txCtx, adjustmentID, actorID, reason, "posted")
		if err != nil {
			return err
		}
		if replayed != nil {
			result = replayed
			return nil
		}
		adjustment, err := s.repo.PrepareCustodyAdjustmentForPosting(txCtx, adjustmentID, actorID)
		if err != nil {
			return err
		}
		if err := s.posting.Post(txCtx, adjustment, func(postCtx context.Context) error {
			return s.repo.MarkCustodyAdjustmentPosted(postCtx, adjustment, actorID, reason)
		}); err != nil {
			return err
		}
		result = adjustment
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("approve custody adjustment: %w", err)
	}
	return result, nil
}

func (s *ExternalOutflowService) RejectAdjustment(ctx context.Context, adjustmentID, actorID id.ID, reason string) (*crypto.CryptoCustodyAdjustment, error) {
	if id.IsNil(adjustmentID) || id.IsNil(actorID) || strings.TrimSpace(reason) == "" {
		return nil, apperror.NewValidation("adjustment, checker and reason are required")
	}
	var result *crypto.CryptoCustodyAdjustment
	err := s.txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		var err error
		result, err = s.repo.ReplayCustodyDecision(txCtx, adjustmentID, actorID, reason, "rejected")
		if err != nil || result != nil {
			return err
		}
		result, err = s.repo.RejectCustodyAdjustment(txCtx, adjustmentID, actorID, reason)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("reject custody adjustment: %w", err)
	}
	return result, nil
}

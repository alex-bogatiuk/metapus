package cryptoapp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"metapus/internal/application/command"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/numerator"
	"metapus/internal/core/security"
	"metapus/internal/core/types"
	pbo "metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/domain/eventlog"
)

// EventProviderOpeningDecision records a provider opening approval or
// rejection. The 00007 migration protects these rows from edits and deletes.
const EventProviderOpeningDecision eventlog.EventType = "crypto.provider_opening.decision"

type ProviderBalanceOpeningEvidence struct {
	ProviderAccountID id.ID
	TokenID           id.ID
	Status            string
	LedgerAmount      types.CryptoDelta
	ProviderAmount    types.CryptoAmount
	DeltaAmount       types.CryptoDelta
	ObservationID     id.ID
	ObservedAt        time.Time
}

// Repositories use the command transaction in ctx. Lock order is always
// account -> document -> incident, shared with provider ledger writers.
type ProviderBalanceWorkflowRepository interface {
	LockAccount(context.Context, id.ID) error
	LoadOpeningEvidence(context.Context, id.ID, id.ID) (ProviderBalanceOpeningEvidence, error)
	FindActiveOpeningByAccountToken(context.Context, id.ID, id.ID) (*id.ID, error)
}

type PrepareProviderOpeningCommand struct {
	IncidentID     id.ID  `json:"incidentId"`
	ObservationID  id.ID  `json:"observationId"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"-"`
}

type ProviderBalanceDecisionCommand struct {
	OperationID    id.ID  `json:"operationId"`
	Version        int    `json:"version"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"-"`
}

type ProviderBalanceResult struct {
	ID                    id.ID            `json:"id"`
	Number                string           `json:"number"`
	Status                string           `json:"status"`
	Version               int              `json:"version"`
	DecisionMode          pbo.DecisionMode `json:"decisionMode,omitempty"`
	DecisionPolicyVersion int              `json:"decisionPolicyVersion,omitzero"`
}

type ProviderBalanceWorkflowService struct {
	repo      pbo.Repository
	workflow  ProviderBalanceWorkflowRepository
	numerator numerator.Generator
	posting   FinancialPostingEngine
	commands  *command.Executor
	stepUp    FreshStepUpChecker
	authority ProviderOpeningAuthorityRepository
	audit     interface {
		Write(context.Context, eventlog.Event) error
	}
	now func() time.Time
}

func NewProviderBalanceWorkflowService(repo pbo.Repository, workflow ProviderBalanceWorkflowRepository,
	numbers numerator.Generator, posting FinancialPostingEngine, commands *command.Executor, stepUp FreshStepUpChecker,
	authority ProviderOpeningAuthorityRepository, audit interface {
		Write(context.Context, eventlog.Event) error
	}) *ProviderBalanceWorkflowService {
	if repo == nil || workflow == nil || numbers == nil || posting == nil || commands == nil || stepUp == nil || authority == nil || audit == nil {
		panic("cryptoapp.NewProviderBalanceWorkflowService: all dependencies are required")
	}
	return &ProviderBalanceWorkflowService{repo: repo, workflow: workflow, numerator: numbers, posting: posting, commands: commands, stepUp: stepUp, authority: authority, audit: audit, now: time.Now}
}

func (s *ProviderBalanceWorkflowService) PrepareOpening(ctx context.Context, req PrepareProviderOpeningCommand) (command.Execution, error) {
	req.Reason = strings.TrimSpace(req.Reason)
	if id.IsNil(req.IncidentID) || id.IsNil(req.ObservationID) || req.Reason == "" || len(req.Reason) > 2000 {
		return command.Execution{}, apperror.NewValidation("incident, observation and reason (up to 2000 bytes) are required")
	}
	return s.execute(ctx, "prepare_opening", "create", req.IdempotencyKey, req, func(txCtx context.Context, actor id.ID) (*pbo.CryptoProviderBalanceOperation, error) {
		evidence, err := s.workflow.LoadOpeningEvidence(txCtx, req.IncidentID, req.ObservationID)
		if err != nil {
			return nil, err
		}
		if evidence.Status != "open" && evidence.Status != "acknowledged" || !evidence.DeltaAmount.IsPositive() {
			return nil, apperror.NewValidation("opening requires an active positive provider balance discrepancy")
		}
		existing, err := s.workflow.FindActiveOpeningByAccountToken(txCtx, evidence.ProviderAccountID, evidence.TokenID)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, apperror.NewValidation("an opening already exists for this provider account and token").WithDetail("operationId", existing.String())
		}
		now := s.now().UTC()
		number, err := s.numerator.GetNextNumber(txCtx, numerator.DefaultConfig("PBO"), nil, now)
		if err != nil {
			return nil, err
		}
		candidate := &pbo.CryptoProviderBalanceOperation{
			Document: entity.NewDocument(), ProviderAccountID: evidence.ProviderAccountID, TokenID: evidence.TokenID,
			ProviderObservationID: &req.ObservationID, OperationType: pbo.OperationTypeOpening, RecordType: entity.RecordTypeReceipt,
			Amount: evidence.DeltaAmount.Abs(), ExternalRef: "opening-observation:" + req.ObservationID.String(), Status: pbo.StatusPendingApproval,
			Evidence: command.MustPayload(evidence),
		}
		candidate.Number, candidate.Date, candidate.Description = number, now, req.Reason
		candidate.BasisType, candidate.BasisID = "CryptoProviderBalanceIncident", &req.IncidentID
		candidate.CreatedBy, candidate.UpdatedBy = actor, actor
		if err := candidate.Validate(txCtx); err != nil {
			return nil, err
		}
		if err := s.recheck(txCtx, "prepare_opening", req); err != nil {
			return nil, err
		}
		if err := s.repo.Create(txCtx, candidate); err != nil {
			return nil, err
		}
		return candidate, nil
	})
}

func (s *ProviderBalanceWorkflowService) Approve(ctx context.Context, req ProviderBalanceDecisionCommand) (command.Execution, error) {
	return s.decide(ctx, req, true)
}
func (s *ProviderBalanceWorkflowService) Reject(ctx context.Context, req ProviderBalanceDecisionCommand) (command.Execution, error) {
	return s.decide(ctx, req, false)
}

func (s *ProviderBalanceWorkflowService) decide(ctx context.Context, req ProviderBalanceDecisionCommand, approve bool) (command.Execution, error) {
	req.Reason = strings.TrimSpace(req.Reason)
	if id.IsNil(req.OperationID) || req.Version < 1 || req.Reason == "" || len(req.Reason) > 2000 {
		return command.Execution{}, apperror.NewValidation("operation, version and reason (up to 2000 bytes) are required")
	}
	action := "reject"
	if approve {
		action = "approve"
	}
	return s.execute(ctx, action, action, req.IdempotencyKey, req, func(txCtx context.Context, actor id.ID) (*pbo.CryptoProviderBalanceOperation, error) {
		snapshot, err := s.repo.GetByID(txCtx, req.OperationID)
		if err != nil {
			return nil, err
		}
		if err = s.workflow.LockAccount(txCtx, snapshot.ProviderAccountID); err != nil {
			return nil, err
		}
		doc, err := s.repo.GetByIDForUpdate(txCtx, req.OperationID)
		if err != nil {
			return nil, err
		}
		authorization, authority, err := s.authorizeDecision(txCtx, doc, actor, action)
		if err != nil {
			return nil, err
		}
		if err = doc.CheckDecision(authorization, req.Version, req.Reason); err != nil {
			return nil, err
		}
		if err = s.recheck(txCtx, action, req); err != nil {
			return nil, err
		}
		if approve {
			if err = doc.Validate(txCtx); err != nil {
				return nil, err
			}
			evidence, evidenceErr := s.workflow.LoadOpeningEvidence(txCtx, *doc.BasisID, *doc.ProviderObservationID)
			if evidenceErr != nil {
				return nil, evidenceErr
			}
			if evidence.ProviderAccountID != doc.ProviderAccountID || evidence.TokenID != doc.TokenID || !evidence.DeltaAmount.IsPositive() || !evidence.DeltaAmount.Abs().Equal(doc.Amount) {
				return nil, apperror.NewValidation("opening evidence does not match the immutable operation")
			}
			if err = doc.Approve(authorization, req.Version, req.Reason, s.now().UTC()); err != nil {
				return nil, err
			}
			if err = s.recheck(txCtx, action, req); err != nil {
				return nil, err
			}
			err = s.posting.Post(txCtx, doc, func(postCtx context.Context) error { return s.repo.Update(postCtx, doc) })
			// Only a subsequent provider observation can prove the new ledger reconciles.
		} else {
			if err = doc.Reject(authorization, req.Version, req.Reason, s.now().UTC()); err != nil {
				return nil, err
			}
			err = s.repo.Update(txCtx, doc)
		}
		if err != nil {
			return nil, err
		}
		checkedAt := s.now().UTC()
		fingerprint, err := command.Fingerprint(req)
		if err != nil {
			return nil, err
		}
		_, session, _, err := requireFreshStepUp(txCtx, s.stepUp, "crypto.provider_balance."+action+":"+fingerprint, checkedAt)
		if err != nil {
			return nil, err
		}
		err = s.audit.Write(txCtx, eventlog.Event{
			Category: eventlog.CategorySecurity, Severity: eventlog.SeverityInfo, EventType: EventProviderOpeningDecision,
			Source: "provider_balance_workflow", EntityType: doc.GetDocumentType(), EntityID: &doc.ID, EntityNumber: doc.Number,
			UserID: actor.String(), SessionID: session.String(), Message: "Provider opening decision recorded", CreatedAt: checkedAt,
			Details: map[string]any{"action": action, "makerId": doc.CreatedBy.String(), "actorId": actor.String(), "decisionMode": authorization.Mode,
				"decisionPolicyVersion": authorization.PolicyVersion, "authorityBasis": authority.Basis, "authorityAuthVersion": authority.AuthVersion,
				"requestFingerprint": fingerprint, "stepUpCheckedAt": checkedAt, "reason": req.Reason},
		})
		return doc, err
	})
}

func (s *ProviderBalanceWorkflowService) execute(ctx context.Context, action, permission, key string, request any,
	fn func(context.Context, id.ID) (*pbo.CryptoProviderBalanceOperation, error)) (command.Execution, error) {
	user := appctx.GetUser(ctx)
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return command.Execution{}, err
	}
	if user == nil {
		return command.Execution{}, apperror.NewUnauthorized("authenticated operator is required")
	}
	if !appctx.IsSystemAdmin(user) && !slices.Contains(user.Permissions, "document:crypto_provider_balance_operation:"+permission) {
		return command.Execution{}, apperror.NewForbidden("provider balance workflow permission is required")
	}
	if strings.TrimSpace(key) == "" {
		return command.Execution{}, apperror.NewIdempotencyKeyRequired()
	}
	if len(key) > 200 {
		return command.Execution{}, apperror.NewValidation("idempotency key is too long")
	}
	fingerprint, err := command.Fingerprint(request)
	if err != nil {
		return command.Execution{}, apperror.NewInternal(err)
	}
	actionCode := "crypto.provider_balance." + action + ":" + fingerprint
	actor, _, tenantID, err := requireFreshStepUp(ctx, s.stepUp, actionCode, s.now().UTC())
	if err != nil {
		return command.Execution{}, err
	}
	execution, err := s.commands.Execute(ctx, command.Metadata{ScopeType: "user", ScopeID: tenantID + ":" + actor.String(), Operation: "crypto.provider_balance." + action, IdempotencyKey: key, RequestFingerprint: fingerprint}, func(txCtx context.Context) (command.Result, error) {
		// Acquisition can wait for another command; recheck freshness before effects.
		if _, _, _, err := requireFreshStepUp(txCtx, s.stepUp, actionCode, s.now().UTC()); err != nil {
			return command.Result{}, err
		}
		doc, err := fn(txCtx, actor)
		if err != nil {
			return command.Result{}, err
		}
		result := ProviderBalanceResult{ID: doc.ID, Number: doc.Number, Status: doc.Status, Version: doc.Version, DecisionMode: doc.DecisionMode, DecisionPolicyVersion: doc.DecisionPolicyVersion}
		return command.Result{Outcome: doc.Status, ResourceType: doc.GetDocumentType(), ResourceID: doc.ID.String(), ResourceVersion: int64(doc.Version), Payload: command.MustPayload(result)}, nil
	})
	if errors.Is(err, command.ErrKeyReused) {
		err = apperror.NewIdempotencyMismatch(key)
	}
	if errors.Is(err, command.ErrInProgress) {
		err = apperror.NewIdempotencyConflict(key).WithDetail("retryAfterSeconds", 3)
	}
	return execution, err
}

func (s *ProviderBalanceWorkflowService) recheck(ctx context.Context, action string, request any) error {
	fingerprint, err := command.Fingerprint(request)
	if err != nil {
		return err
	}
	_, _, _, err = requireFreshStepUp(ctx, s.stepUp, "crypto.provider_balance."+action+":"+fingerprint, s.now().UTC())
	return err
}

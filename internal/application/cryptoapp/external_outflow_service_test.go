package cryptoapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/core/types"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/posting"
)

type immediateTxManager struct {
	calls int
}

func (m *immediateTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	return fn(ctx)
}

type externalOutflowRepoStub struct {
	outflow           crypto.ExternalOutflow
	exactBasis        bool
	falsePositive     bool
	freshReconcile    bool
	classifyCalls     int
	adjustmentCreates int
	resolveCalls      int
	postingCalls      int
	classificationKey string
	resolutionKnown   bool
	adjustmentKnown   bool
}

func classificationTestKey(command ClassifyExternalOutflowCommand, actor id.ID) (string, error) {
	data, err := json.MarshalDeterministic(struct {
		Command ClassifyExternalOutflowCommand
		Actor   id.ID
	}{command, actor})
	return string(data), err
}

func (r *externalOutflowRepoStub) ReplayClassification(_ context.Context, _ *crypto.ExternalOutflow, command ClassifyExternalOutflowCommand, actor id.ID) (bool, error) {
	key, err := classificationTestKey(command, actor)
	return r.classificationKey != "" && r.classificationKey == key, err
}

func (r *externalOutflowRepoStub) ReplayResolution(_ context.Context, outflow *crypto.ExternalOutflow, actor id.ID, reason string) (bool, error) {
	if outflow.Status != crypto.ExternalOutflowResolved {
		return false, nil
	}
	if r.resolutionKnown && outflow.ResolvedBy != nil && *outflow.ResolvedBy == actor && outflow.Resolution == reason {
		return true, nil
	}
	return false, apperror.NewConflict("resolution replay differs")
}

func (r *externalOutflowRepoStub) ReplayCustodyDecision(_ context.Context, document, actor id.ID, reason, status string) (*crypto.CryptoCustodyAdjustment, error) {
	a := r.outflow.Adjustment
	if a == nil || a.ID != document {
		return nil, apperror.NewNotFound("custody adjustment", document.String())
	}
	if a.Status == "pending_approval" {
		return nil, nil
	}
	if r.adjustmentKnown && a.Status == status && a.DecisionBy != nil && *a.DecisionBy == actor && a.DecisionReason == reason {
		copy := *a
		return &copy, nil
	}
	return nil, apperror.NewConflict("adjustment replay differs")
}

func (r *externalOutflowRepoStub) ListExternalOutflows(context.Context, string, int) ([]crypto.ExternalOutflow, error) {
	return []crypto.ExternalOutflow{r.outflow}, nil
}

func (r *externalOutflowRepoStub) GetExternalOutflow(_ context.Context, outflowID id.ID, _ bool) (*crypto.ExternalOutflow, error) {
	if outflowID != r.outflow.ID {
		return nil, apperror.NewNotFound("external_outflow", outflowID.String())
	}
	copy := r.outflow
	return &copy, nil
}

func (r *externalOutflowRepoStub) MatchExactBasis(context.Context, *crypto.ExternalOutflow, string, id.ID) (bool, error) {
	return r.exactBasis, nil
}

func (r *externalOutflowRepoStub) HasFalsePositiveProof(context.Context, *crypto.ExternalOutflow) (bool, error) {
	return r.falsePositive, nil
}

func (r *externalOutflowRepoStub) ClassifyExternalOutflow(_ context.Context, _ *crypto.ExternalOutflow, command ClassifyExternalOutflowCommand, actorID id.ID) error {
	key, err := classificationTestKey(command, actorID)
	if err != nil {
		return err
	}
	r.classificationKey = key
	r.classifyCalls++
	r.outflow.Status = crypto.ExternalOutflowClassified
	r.outflow.Classification = command.Classification
	r.outflow.ClassifiedBy = &actorID
	r.outflow.ClassificationReason = command.Reason
	r.outflow.MatchedBasisType = command.MatchedBasisType
	r.outflow.MatchedBasisID = command.MatchedBasisID
	return nil
}

func (r *externalOutflowRepoStub) CreateCustodyAdjustment(_ context.Context, outflow *crypto.ExternalOutflow, actorID id.ID) (*crypto.CryptoCustodyAdjustment, error) {
	r.adjustmentCreates++
	adjustment := &crypto.CryptoCustodyAdjustment{
		ID: id.New(), ExternalOutflowID: outflow.ID, NetworkID: outflow.NetworkID,
		WalletID: *outflow.SourceWalletID, TokenID: *outflow.TokenID, Amount: outflow.Amount,
		Status: "pending_approval", MakerID: actorID,
		// The repository admits the proven outflow before the checker posts it.
		RecognitionPeriod: time.Now().UTC().Truncate(time.Microsecond),
	}
	r.outflow.Adjustment = adjustment
	return adjustment, nil
}

func (r *externalOutflowRepoStub) HasFreshSuccessfulReconciliation(context.Context, *crypto.ExternalOutflow) (bool, error) {
	return r.freshReconcile, nil
}

func (r *externalOutflowRepoStub) ResolveExternalOutflow(_ context.Context, _ *crypto.ExternalOutflow, actorID id.ID, reason string) error {
	r.resolveCalls++
	r.outflow.Status = crypto.ExternalOutflowResolved
	r.outflow.ResolvedBy = &actorID
	r.outflow.Resolution = reason
	r.resolutionKnown = true
	return nil
}

func (r *externalOutflowRepoStub) PrepareCustodyAdjustmentForPosting(_ context.Context, adjustmentID, actorID id.ID) (*crypto.CryptoCustodyAdjustment, error) {
	if r.outflow.Adjustment == nil || r.outflow.Adjustment.ID != adjustmentID {
		return nil, apperror.NewNotFound("crypto_custody_adjustment", adjustmentID.String())
	}
	if r.outflow.Adjustment.MakerID == actorID {
		return nil, apperror.NewForbidden("custody adjustment maker cannot approve it")
	}
	return r.outflow.Adjustment, nil
}

func (r *externalOutflowRepoStub) MarkCustodyAdjustmentPosted(_ context.Context, adjustment *crypto.CryptoCustodyAdjustment, actorID id.ID, reason string) error {
	r.outflow.Adjustment.DecisionBy = &actorID
	r.outflow.Adjustment.DecisionReason = reason
	r.outflow.Adjustment.Status = adjustment.Status
	r.outflow.Adjustment.PostedVersion = adjustment.PostedVersion
	r.adjustmentKnown = true
	return nil
}

func (r *externalOutflowRepoStub) RejectCustodyAdjustment(_ context.Context, adjustmentID, actorID id.ID, reason string) (*crypto.CryptoCustodyAdjustment, error) {
	if r.outflow.Adjustment == nil || r.outflow.Adjustment.ID != adjustmentID {
		return nil, apperror.NewNotFound("crypto_custody_adjustment", adjustmentID.String())
	}
	if r.outflow.Adjustment.MakerID == actorID {
		return nil, apperror.NewForbidden("maker cannot reject adjustment")
	}
	r.outflow.Adjustment.Status = "rejected"
	r.outflow.Adjustment.DecisionBy = &actorID
	r.outflow.Adjustment.DecisionReason = reason
	r.adjustmentKnown = true
	copy := *r.outflow.Adjustment
	return &copy, nil
}

func newExternalOutflowRepoStub() *externalOutflowRepoStub {
	networkID, walletID, tokenID := id.New(), id.New(), id.New()
	return &externalOutflowRepoStub{outflow: crypto.ExternalOutflow{
		ID: id.New(), NetworkID: networkID, SourceWalletID: &walletID, TokenID: &tokenID,
		Status: crypto.ExternalOutflowPendingReview, Amount: types.NewRawAmountFromInt64(1250),
		FinalityStatus: "finalized", Confirmations: 20, RequiredConfirmations: 20,
	}}
}

func requireAppErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Code != code {
		t.Fatalf("expected code %s, got %s", code, appErr.Code)
	}
}

func newExternalOutflowServiceForTest(txm *immediateTxManager, repo *externalOutflowRepoStub) (*ExternalOutflowService, *operationalHoldRepoStub) {
	holdRepo := &operationalHoldRepoStub{}
	holds := NewOperationalHoldService(txm, holdRepo)
	return NewExternalOutflowService(txm, repo, holds, immediatePostingEngine{calls: &repo.postingCalls}), holdRepo
}

type immediatePostingEngine struct{ calls *int }

func (e immediatePostingEngine) Post(ctx context.Context, document posting.Postable, update func(context.Context) error) error {
	if e.calls != nil {
		*e.calls++
	}
	if err := document.CanPost(ctx); err != nil {
		return err
	}
	document.MarkPosted()
	return update(ctx)
}

func TestApproveCustodyAdjustmentUsesPostingEngineAndEnforcesMakerChecker(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, _ := newExternalOutflowServiceForTest(txm, repo)
	makerID, checkerID := id.New(), id.New()

	classified, err := service.Classify(context.Background(), ClassifyExternalOutflowCommand{
		OutflowID:      repo.outflow.ID,
		Classification: crypto.OutflowUnauthorizedLoss,
		Reason:         "custody loss confirmed",
	}, makerID)
	if err != nil {
		t.Fatalf("classify unauthorized loss: %v", err)
	}
	adjustmentID := classified.Adjustment.ID

	adminCtx := appctx.WithUser(context.Background(), &appctx.UserContext{UserID: makerID.String(), IsAdmin: true, Roles: []string{"admin"}})
	_, err = service.ApproveAdjustment(adminCtx, adjustmentID, makerID, "self approval")
	requireAppErrorCode(t, err, apperror.CodeForbidden)
	if repo.postingCalls != 0 {
		t.Fatalf("maker-checker failure invoked posting engine %d times", repo.postingCalls)
	}

	adjustment, err := service.ApproveAdjustment(context.Background(), adjustmentID, checkerID, "custody accounting approved")
	if err != nil {
		t.Fatalf("approve adjustment: %v", err)
	}
	if repo.postingCalls != 1 {
		t.Fatalf("expected exactly one posting call, got %d", repo.postingCalls)
	}
	if adjustment.Status != "posted" || adjustment.PostedVersion != 1 {
		t.Fatalf("expected posted version 1, got status=%q version=%d", adjustment.Status, adjustment.PostedVersion)
	}
	if adjustment.DecisionBy == nil || *adjustment.DecisionBy != checkerID {
		t.Fatalf("expected checker decision, got %#v", adjustment.DecisionBy)
	}
}

func TestExternalOutflowClassifyMatchedBasisRequiresExactEffect(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, _ := newExternalOutflowServiceForTest(txm, repo)
	basisID := id.New()

	_, err := service.Classify(context.Background(), ClassifyExternalOutflowCommand{
		OutflowID: repo.outflow.ID, Classification: crypto.OutflowMatchedBasis,
		MatchedBasisType: "treasury_withdrawal", MatchedBasisID: &basisID, Reason: "operator review",
	}, id.New())

	requireAppErrorCode(t, err, apperror.CodeValidation)
	if repo.classifyCalls != 0 || repo.adjustmentCreates != 0 {
		t.Fatalf("classification mutated state without exact proof: classify=%d adjustments=%d", repo.classifyCalls, repo.adjustmentCreates)
	}
}

func TestExternalOutflowUnauthorizedLossCreatesSingleAdjustment(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, _ := newExternalOutflowServiceForTest(txm, repo)
	makerID := id.New()

	result, err := service.Classify(context.Background(), ClassifyExternalOutflowCommand{
		OutflowID: repo.outflow.ID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "custody loss confirmed",
	}, makerID)
	if err != nil {
		t.Fatalf("classify unauthorized loss: %v", err)
	}
	if result.Adjustment == nil || result.Adjustment.Status != "pending_approval" {
		t.Fatalf("expected pending custody adjustment, got %#v", result.Adjustment)
	}
	if repo.classifyCalls != 1 || repo.adjustmentCreates != 1 || txm.calls != 1 {
		t.Fatalf("unexpected economic effects: classify=%d adjustments=%d tx=%d", repo.classifyCalls, repo.adjustmentCreates, txm.calls)
	}

	_, err = service.Classify(context.Background(), ClassifyExternalOutflowCommand{
		OutflowID: repo.outflow.ID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "retry",
	}, makerID)
	requireAppErrorCode(t, err, apperror.CodeConflict)
	if repo.adjustmentCreates != 1 {
		t.Fatalf("retry created a second adjustment: %d", repo.adjustmentCreates)
	}
}

func TestExternalOutflowUnauthorizedLossRequiresFinality(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, _ := newExternalOutflowServiceForTest(txm, repo)
	repo.outflow.FinalityStatus = "observed"
	repo.outflow.Confirmations = 1

	_, err := service.Classify(context.Background(), ClassifyExternalOutflowCommand{
		OutflowID: repo.outflow.ID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "suspected custody loss",
	}, id.New())

	requireAppErrorCode(t, err, apperror.CodeValidation)
	if repo.classifyCalls != 0 || repo.adjustmentCreates != 0 {
		t.Fatalf("non-final outflow mutated financial state: classify=%d adjustments=%d", repo.classifyCalls, repo.adjustmentCreates)
	}
}

func TestExternalOutflowResolutionEnforcesMakerCheckerAdjustmentAndFreshReconcile(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, holdRepo := newExternalOutflowServiceForTest(txm, repo)
	classifierID, resolverID := id.New(), id.New()
	repo.outflow.Status = crypto.ExternalOutflowClassified
	repo.outflow.Classification = crypto.OutflowUnauthorizedLoss
	repo.outflow.ClassifiedBy = &classifierID
	repo.outflow.Adjustment = &crypto.CryptoCustodyAdjustment{ID: id.New(), Status: "pending_approval"}

	_, err := service.Resolve(context.Background(), repo.outflow.ID, classifierID, "same actor")
	requireAppErrorCode(t, err, apperror.CodeForbidden)

	_, err = service.Resolve(context.Background(), repo.outflow.ID, resolverID, "not posted")
	requireAppErrorCode(t, err, apperror.CodeConflict)

	repo.outflow.Adjustment.Status = "posted"
	_, err = service.Resolve(context.Background(), repo.outflow.ID, resolverID, "not reconciled")
	requireAppErrorCode(t, err, apperror.CodeConflict)

	repo.freshReconcile = true
	repo.outflow.FinalityStatus = "reorged"
	_, err = service.Resolve(context.Background(), repo.outflow.ID, resolverID, "stale custody loss")
	requireAppErrorCode(t, err, apperror.CodeConflict)
	if repo.resolveCalls != 0 || holdRepo.resolved != 0 {
		t.Fatalf("reorged custody loss changed state: resolutions=%d holdClosures=%d", repo.resolveCalls, holdRepo.resolved)
	}

	repo.outflow.FinalityStatus = "finalized"
	result, err := service.Resolve(context.Background(), repo.outflow.ID, resolverID, "ledger matches custody")
	if err != nil {
		t.Fatalf("resolve classified loss: %v", err)
	}
	if result.Status != crypto.ExternalOutflowResolved || repo.resolveCalls != 1 {
		t.Fatalf("expected one resolution, got status=%s calls=%d", result.Status, repo.resolveCalls)
	}
	if holdRepo.owner != "external_outflow:"+repo.outflow.ID.String() || holdRepo.resolved != 1 {
		t.Fatalf("resolution did not selectively close owner holds: owner=%q calls=%d", holdRepo.owner, holdRepo.resolved)
	}
}

func TestExternalOutflowResolutionReadinessReportsReorgedCustodyLoss(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, _ := newExternalOutflowServiceForTest(txm, repo)
	classifierID := id.New()
	repo.outflow.Status = crypto.ExternalOutflowClassified
	repo.outflow.Classification = crypto.OutflowUnauthorizedLoss
	repo.outflow.ClassifiedBy = &classifierID
	repo.outflow.Adjustment = &crypto.CryptoCustodyAdjustment{Status: "posted"}
	repo.outflow.FinalityStatus = "reorged"
	repo.freshReconcile = true

	readiness, err := service.ResolutionReadiness(context.Background(), &repo.outflow, id.New())
	if err != nil {
		t.Fatalf("resolution readiness: %v", err)
	}
	if readiness.CanResolve || len(readiness.Blockers) != 1 || readiness.Blockers[0] != "custody loss chain effect is no longer final" {
		t.Fatalf("expected finality blocker, got %#v", readiness)
	}
}

func TestExternalOutflowRejectedAdjustmentCanOnlyReclassifyWithoutNewLoss(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, _ := newExternalOutflowServiceForTest(txm, repo)
	classifierID := id.New()
	repo.outflow.Status = crypto.ExternalOutflowClassified
	repo.outflow.Classification = crypto.OutflowUnauthorizedLoss
	repo.outflow.ClassifiedBy = &classifierID
	repo.outflow.Adjustment = &crypto.CryptoCustodyAdjustment{ID: id.New(), Status: "rejected"}

	_, err := service.Classify(context.Background(), ClassifyExternalOutflowCommand{
		OutflowID: repo.outflow.ID, Classification: crypto.OutflowUnauthorizedLoss, Reason: "try loss again",
	}, id.New())
	requireAppErrorCode(t, err, apperror.CodeValidation)

	repo.falsePositive = true
	result, err := service.Classify(context.Background(), ClassifyExternalOutflowCommand{
		OutflowID: repo.outflow.ID, Classification: crypto.OutflowFalsePositiveOrReorg, Reason: "removed log proved",
	}, id.New())
	if err != nil {
		t.Fatalf("reclassify rejected adjustment: %v", err)
	}
	if result.Classification != crypto.OutflowFalsePositiveOrReorg || repo.adjustmentCreates != 0 {
		t.Fatalf("unexpected reclassification result: classification=%s adjustments=%d", result.Classification, repo.adjustmentCreates)
	}
}

func TestExternalOutflowResolutionReadinessReturnsAllOperationalBlockers(t *testing.T) {
	repo, txm := newExternalOutflowRepoStub(), &immediateTxManager{}
	service, _ := newExternalOutflowServiceForTest(txm, repo)
	classifierID := id.New()
	repo.outflow.Status = crypto.ExternalOutflowClassified
	repo.outflow.Classification = crypto.OutflowUnauthorizedLoss
	repo.outflow.ClassifiedBy = &classifierID
	repo.outflow.Adjustment = &crypto.CryptoCustodyAdjustment{Status: "pending_approval"}

	readiness, err := service.ResolutionReadiness(context.Background(), &repo.outflow, classifierID)
	if err != nil {
		t.Fatalf("resolution readiness: %v", err)
	}
	if readiness.CanResolve || len(readiness.Blockers) != 3 {
		t.Fatalf("expected maker-checker, adjustment and reconciliation blockers, got %#v", readiness)
	}
}

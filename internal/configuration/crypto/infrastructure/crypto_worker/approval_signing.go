package crypto_worker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/infrastructure/crypto/approval_signer"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/configuration/crypto/infrastructure/vaultclient"
	"metapus/pkg/sdk/json"
	"metapus/pkg/sdk/telemetry"
	"metapus/pkg/sdk/tenant"
)

const _approvalSigningPoll = 3 * time.Second
const _approvalSigningLease = 5 * time.Minute

func (p *CryptoProcessor) runApprovalSigningLoop(ctx context.Context) {
	ticker := time.NewTicker(_approvalSigningPoll)
	defer ticker.Stop()
	for {
		if err := p.processApprovalSigningJobs(ctx); err != nil && ctx.Err() == nil {
			p.log.Errorw("approval signing jobs failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *CryptoProcessor) processApprovalSigningJobs(ctx context.Context) error {
	var jobs []money_movement_repo.SigningJob
	err := (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		var claimErr error
		jobs, claimErr = p.approvalRepo.ClaimSigningJobs(txCtx, p.workerID, 10, _approvalSigningLease)
		return claimErr
	})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err := p.processClaimedApprovalSigningJob(ctx, job); err != nil {
			return err
		}
	}
	return nil
}

func (p *CryptoProcessor) processClaimedApprovalSigningJob(ctx context.Context, job money_movement_repo.SigningJob) error {
	jobCtx := telemetry.ContinueTrace(ctx, job.Traceparent)
	jobCtx = telemetry.WithBusinessContext(jobCtx, job.CorrelationID, job.CausationID)
	jobCtx, span := otel.Tracer("metapus/crypto/approval-signing").Start(jobCtx,
		"approval.signing_job", oteltrace.WithSpanKind(oteltrace.SpanKindConsumer),
		oteltrace.WithAttributes(
			attribute.String("job.id", job.ID.String()),
			attribute.String("approval.basis_type", job.BasisType),
			attribute.String("approval.basis_id", job.BasisID.String()),
			attribute.String("business.correlation_id", job.CorrelationID),
			attribute.String("business.causation_id", job.CausationID),
		))
	defer span.End()
	if err := p.processApprovalSigningJob(jobCtx, job); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "approval signing failed")
		p.log.Errorw("withdrawal approval signing failed", "job_id", job.ID,
			"basis_id", job.BasisID, "attempts", job.Attempts, "error", err)
		failErr := (contextTxManager{}).RunInTransaction(jobCtx, func(txCtx context.Context) error {
			needsReview, releaseErr := p.approvalRepo.FailSigningJob(txCtx, job.ID, p.workerID, err)
			if needsReview {
				p.log.Errorw("withdrawal signing needs manual review", "job_id", job.ID,
					"basis_id", job.BasisID)
			}
			return releaseErr
		})
		if failErr != nil {
			return fmt.Errorf("release approval signing job: %w", failErr)
		}
	}
	return nil
}

func (p *CryptoProcessor) processApprovalSigningJob(ctx context.Context, job money_movement_repo.SigningJob) error {
	var intent crypto.MoneyMovementIntent
	if err := json.Unmarshal(job.IntentPayload, &intent); err != nil {
		return fmt.Errorf("decode frozen signing intent: %w", err)
	}
	canonical, err := json.MarshalDeterministic(intent)
	if err != nil {
		return fmt.Errorf("encode frozen signing intent: %w", err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(canonical)) != job.IntentFingerprint {
		return fmt.Errorf("frozen signing intent fingerprint mismatch")
	}
	if intent.BasisType != job.BasisType || intent.BasisID != job.BasisID ||
		intent.TenantID != tenant.GetTenantID(ctx) {
		return fmt.Errorf("signing job basis or tenant mismatch")
	}
	status, err := p.approvalRepo.SigningBasisStatus(ctx, job.BasisType, job.BasisID, false)
	if err != nil {
		return err
	}
	if status != "pending_signature" {
		return (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
			if err := p.approvalRepo.LockSigningJob(txCtx, job.ID, p.workerID); err != nil {
				return err
			}
			return p.approvalRepo.CompleteSigningJob(txCtx, job.ID, p.workerID, true)
		})
	}
	provider, err := p.resolveApprovalSigningProvider(ctx)
	if err != nil {
		return err
	}
	approvals := cryptoapp.NewMoneyMovementApprovalService(p.approvalRepo, provider, nil)
	artifacts, err := approvals.BuildArtifacts(ctx, intent)
	if err != nil {
		return err
	}
	return (contextTxManager{}).RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := p.approvalRepo.LockSigningJob(txCtx, job.ID, p.workerID); err != nil {
			return err
		}
		lockedStatus, err := p.approvalRepo.SigningBasisStatus(txCtx, job.BasisType, job.BasisID, true)
		if err != nil {
			return err
		}
		if lockedStatus != "pending_signature" {
			return p.approvalRepo.CompleteSigningJob(txCtx, job.ID, p.workerID, true)
		}
		var current crypto.MoneyMovementIntent
		switch job.BasisType {
		case withdrawal_request.DocumentType:
			current, err = p.approvalRepo.BuildWithdrawalIntent(txCtx, tenant.GetTenantID(txCtx), job.BasisID)
		case treasury_withdrawal.DocumentType:
			current, err = p.approvalRepo.BuildTreasuryIntent(txCtx, tenant.GetTenantID(txCtx), job.BasisID)
		}
		if err != nil {
			return err
		}
		if !cryptoapp.MoneyMovementIntentMatches(intent, current) {
			return fmt.Errorf("withdrawal changed after signing intent was captured")
		}
		if err := approvals.SaveArtifacts(txCtx, artifacts); err != nil {
			return err
		}
		nextStatus := "pending_approval"
		if !approvals.RequiresAdmin(artifacts) {
			nextStatus = "approved"
		}
		if err := p.approvalRepo.PublishSigningBasis(txCtx, job.BasisType, job.BasisID, nextStatus); err != nil {
			return err
		}
		return p.approvalRepo.CompleteSigningJob(txCtx, job.ID, p.workerID, false)
	})
}

func (p *CryptoProcessor) resolveApprovalSigningProvider(ctx context.Context) (crypto.MoneyMovementApprovalSignerProvider, error) {
	p.approvalSigningMu.Lock()
	defer p.approvalSigningMu.Unlock()
	if p.approvalSigningProvider != nil {
		return p.approvalSigningProvider, nil
	}
	if strings.TrimSpace(p.cfg.VaultAddr) == "" {
		return nil, fmt.Errorf("vault endpoint is required for approval signing jobs")
	}
	policies := map[string]string{}
	if raw := strings.TrimSpace(p.cfg.ApprovalPolicyBundlesJWS); raw != "" {
		if err := json.Unmarshal([]byte(raw), &policies); err != nil {
			return nil, fmt.Errorf("decode approval policy bundles: %w", err)
		}
	} else if raw := strings.TrimSpace(p.cfg.ApprovalPolicyBundleJWS); raw != "" {
		policies["default"] = raw
	}
	if len(policies) == 0 {
		return nil, fmt.Errorf("signed approval policy bundle is required for signing jobs")
	}
	key, err := crypto.ParseEd25519PublicKey(p.cfg.ApprovalPolicyPublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse approval policy public key: %w", err)
	}
	client, err := vaultclient.New(vaultclient.Config{Addr: p.cfg.VaultAddr, Token: p.cfg.VaultToken})
	if err != nil {
		return nil, err
	}
	provider, err := approval_signer.NewTransitProvider(ctx, approval_signer.TransitProviderConfig{
		Client: client, Mount: p.cfg.TransitMount, PolicyPublicKey: key, PolicyJWS: policies,
	})
	if err != nil {
		return nil, err
	}
	p.approvalSigningProvider = provider
	return provider, nil
}

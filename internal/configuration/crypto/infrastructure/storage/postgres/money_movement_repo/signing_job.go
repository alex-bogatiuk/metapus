package money_movement_repo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/telemetry"
)

type SigningJob struct {
	ID                id.ID           `db:"id"`
	BasisType         string          `db:"basis_type"`
	BasisID           id.ID           `db:"basis_id"`
	IntentPayload     json.RawMessage `db:"intent_payload"`
	IntentFingerprint string          `db:"intent_fingerprint"`
	Traceparent       string          `db:"traceparent"`
	CorrelationID     string          `db:"correlation_id"`
	CausationID       string          `db:"causation_id"`
	Attempts          int             `db:"attempts"`
}

type SigningJobStatus struct {
	Status    string `json:"status" db:"status"`
	Attempts  int    `json:"attempts" db:"attempts"`
	LastError string `json:"lastError" db:"last_error"`
}

func (r *Repo) GetSigningJobStatus(ctx context.Context, basisType string, basisID id.ID) (SigningJobStatus, error) {
	var result SigningJobStatus
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT status,attempts,last_error FROM reg_money_movement_signing_jobs
		WHERE basis_type=$1 AND basis_id=$2
	`, basisType, basisID).Scan(&result.Status, &result.Attempts, &result.LastError)
	if err == pgx.ErrNoRows {
		return result, apperror.NewNotFound("approval signing job", basisID.String())
	}
	return result, err
}

func (r *Repo) ReplaySigningJob(ctx context.Context, basisType string, basisID, actorID id.ID) error {
	if id.IsNil(actorID) {
		return apperror.NewUnauthorized("authenticated operator is required for signing replay")
	}
	var basisTable string
	switch basisType {
	case "WithdrawalRequest":
		basisTable = "doc_withdrawal_requests"
	case "TreasuryWithdrawal":
		basisTable = "doc_treasury_withdrawals"
	default:
		return apperror.NewValidation("unsupported signing job basis")
	}
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		WITH eligible AS MATERIALIZED (
			SELECT job.id,job.last_error
			FROM reg_money_movement_signing_jobs job
			WHERE job.basis_type=$1 AND job.basis_id=$2 AND job.status='needs_review'
			  AND EXISTS (SELECT 1 FROM `+basisTable+` basis
			              WHERE basis.id=job.basis_id AND basis.status='pending_signature'
			                AND basis._deleted_at IS NULL)
			FOR UPDATE
		), replayed AS (
			UPDATE reg_money_movement_signing_jobs job
			SET status='pending',attempts=0,next_attempt_at=NOW(),last_error='',
			    lease_owner=NULL,lease_expires_at=NULL
			FROM eligible WHERE job.id=eligible.id
			RETURNING eligible.id,eligible.last_error
		)
		INSERT INTO reg_money_movement_signing_job_replays(job_id,actor_id,previous_error)
		SELECT id,$3,last_error FROM replayed
	`, basisType, basisID, actorID)
	if err != nil {
		return fmt.Errorf("replay signing job: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewConflict("signing job is not eligible for replay")
	}
	return nil
}

func (r *Repo) EnqueueApprovalSigning(ctx context.Context, intent crypto.MoneyMovementIntent) error {
	payload, err := json.MarshalDeterministic(intent)
	if err != nil {
		return fmt.Errorf("encode approval signing intent: %w", err)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(payload))
	_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO reg_money_movement_signing_jobs
		(basis_type,basis_id,intent_payload,intent_fingerprint,traceparent,correlation_id,causation_id)
		VALUES ($1,$2,$3::jsonb,$4,$5,$6,$7)
	`, intent.BasisType, intent.BasisID, string(payload), fingerprint,
		telemetry.Traceparent(ctx), intent.IntentID, intent.BasisID.String())
	if err != nil {
		return fmt.Errorf("enqueue approval signing: %w", err)
	}
	return nil
}

func (r *Repo) ClaimSigningJobs(ctx context.Context, owner string, limit int, lease time.Duration) ([]SigningJob, error) {
	if owner == "" || limit <= 0 || lease <= 0 {
		return nil, fmt.Errorf("signing job claim requires owner, limit and lease")
	}
	var jobs []SigningJob
	err := pgxscan.Select(ctx, postgres.MustGetTxManager(ctx).GetQuerier(ctx), &jobs, `
		UPDATE reg_money_movement_signing_jobs job
		SET status='processing',attempts=job.attempts+1,
		    lease_owner=$1,lease_expires_at=NOW()+$3::interval
		WHERE job.id IN (
		    SELECT id FROM reg_money_movement_signing_jobs
		    WHERE (status='pending' AND next_attempt_at<=NOW())
		       OR (status='processing' AND lease_expires_at<NOW())
		    ORDER BY next_attempt_at,id FOR UPDATE SKIP LOCKED LIMIT $2
		)
		RETURNING job.id,job.basis_type,job.basis_id,job.intent_payload,job.intent_fingerprint,
		          job.traceparent,job.correlation_id,job.causation_id,job.attempts
	`, owner, limit, fmt.Sprintf("%d seconds", int(lease.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("claim approval signing jobs: %w", err)
	}
	return jobs, nil
}

func (r *Repo) LockSigningJob(ctx context.Context, jobID id.ID, owner string) error {
	var locked id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT id FROM reg_money_movement_signing_jobs
		WHERE id=$1 AND status='processing' AND lease_owner=$2 AND lease_expires_at>NOW()
		FOR UPDATE
	`, jobID, owner).Scan(&locked)
	if err == pgx.ErrNoRows {
		return fmt.Errorf("approval signing job lease lost")
	}
	return err
}

func (r *Repo) CompleteSigningJob(ctx context.Context, jobID id.ID, owner string, cancelled bool) error {
	status := "completed"
	if cancelled {
		status = "cancelled"
	}
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_money_movement_signing_jobs
		SET status=$3,lease_owner=NULL,lease_expires_at=NULL,completed_at=NOW(),last_error=''
		WHERE id=$1 AND status='processing' AND lease_owner=$2 AND lease_expires_at>NOW()
	`, jobID, owner, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("approval signing job lease lost")
	}
	return nil
}

func (r *Repo) FailSigningJob(ctx context.Context, jobID id.ID, owner string, processErr error) (bool, error) {
	message := processErr.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	var needsReview bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		UPDATE reg_money_movement_signing_jobs
		SET status=CASE WHEN attempts>=8 THEN 'needs_review' ELSE 'pending' END,
		    next_attempt_at=NOW()+(LEAST(300,5*(1 << LEAST(attempts,6)))+FLOOR(RANDOM()*3))*INTERVAL '1 second',
		    lease_owner=NULL,lease_expires_at=NULL,last_error=$3
		WHERE id=$1 AND status='processing' AND lease_owner=$2 AND lease_expires_at>NOW()
		RETURNING status='needs_review'
	`, jobID, owner, message).Scan(&needsReview)
	if err != nil {
		return false, fmt.Errorf("release failed signing job: %w", err)
	}
	return needsReview, nil
}

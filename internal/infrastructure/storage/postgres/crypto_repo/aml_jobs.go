package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto/aml"
	"metapus/internal/infrastructure/storage/postgres"
)

type AMLRepository struct{}

func NewAMLRepository() *AMLRepository { return &AMLRepository{} }

type EnqueueAMLInput struct {
	WalletID          id.ID
	Provider          string
	ProviderAccountID id.ID
	MappingVersion    string
	Request           aml.CheckRequest
	DeadlineAt        time.Time
}

// EnqueueAMLCheck requires a previously established custody owner. It never
// guesses ownership from the last invoice or silently claims a legacy wallet.
func (r *AMLRepository) EnqueueAMLCheck(ctx context.Context, input EnqueueAMLInput) (id.ID, error) {
	if err := input.Request.Validate(); err != nil {
		return id.Nil(), err
	}
	if id.IsNil(input.WalletID) || id.IsNil(input.ProviderAccountID) || (input.Provider != "" && input.Provider != "getblock_net" && input.Provider != aml.TestnetProvider) || input.MappingVersion == "" || !input.DeadlineAt.After(time.Now()) {
		return id.Nil(), apperror.NewValidation("complete pinned AML job context and future deadline are required")
	}
	payload, err := json.Marshal(input.Request)
	if err != nil {
		return id.Nil(), fmt.Errorf("encode AML subject: %w", err)
	}
	digest := input.Request.Digest(input.MappingVersion)
	txm := postgres.MustGetTxManager(ctx)
	var jobID id.ID
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		var actualProvider string
		if err := q.QueryRow(txCtx, `SELECT provider FROM cat_integration_accounts WHERE id=$1`, input.ProviderAccountID).Scan(&actualProvider); err != nil {
			return err
		}
		if (actualProvider != "getblock_net" && actualProvider != aml.TestnetProvider) || (input.Provider != "" && input.Provider != actualProvider) {
			return apperror.NewValidation("AML job provider does not match its account")
		}
		input.Provider = actualProvider

		var subjectID id.ID
		var generation int64
		err := q.QueryRow(txCtx, `INSERT INTO reg_crypto_aml_subjects
			(merchant_id,network_id,token_id,wallet_id,subject_kind,request_digest,request_payload,mapping_version)
			SELECT $1,$2,$3,w.id,$5,$6,$7::jsonb,$8 FROM cat_wallets w JOIN cat_tokens t ON t.id=$3 AND t.network_id=w.network_id
			JOIN cat_blockchain_networks n ON n.id=w.network_id
			WHERE w.id=$4 AND w.custody_merchant_id=$1 AND w.network_id=$2
			  AND w.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($9) ELSE $9 END
			  AND CASE WHEN n.chain_family='evm' THEN LOWER(COALESCE(t.contract_address,''))=LOWER($10) ELSE COALESCE(t.contract_address,'')=$10 END
			ON CONFLICT(merchant_id,network_id,token_id,request_digest) DO UPDATE SET updated_at=NOW()
			RETURNING id,generation`, input.Request.MerchantID, input.Request.NetworkID, input.Request.TokenID, input.WalletID,
			string(input.Request.Kind), digest, string(payload), input.MappingVersion, input.Request.Address, input.Request.ContractAddress).Scan(&subjectID, &generation)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewValidation("AML subject does not match immutable merchant wallet and token identity")
		}
		if err != nil {
			return fmt.Errorf("create AML subject: %w", err)
		}
		err = q.QueryRow(txCtx, `INSERT INTO reg_crypto_aml_jobs
			(subject_id,generation,provider,provider_account_id,request_payload,request_digest,mapping_version,submit_request_id,deadline_at,next_attempt_at)
			VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10)
			ON CONFLICT(subject_id,generation,provider) DO UPDATE SET updated_at=NOW()
            WHERE reg_crypto_aml_jobs.provider_account_id=EXCLUDED.provider_account_id
			RETURNING id`, subjectID, generation, input.Provider, input.ProviderAccountID, string(payload), digest, input.MappingVersion,
			id.Compact(id.New()), input.DeadlineAt, time.Now().UTC()).Scan(&jobID)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NewConflict("AML generation is already pinned to another provider account")
		}
		if err != nil {
			return fmt.Errorf("enqueue AML job: %w", err)
		}
		return nil
	})
	return jobID, err
}

func (r *AMLRepository) ClaimAMLJob(ctx context.Context, now time.Time, lease time.Duration) (aml.Job, bool, error) {
	if lease <= 0 || lease > 5*time.Minute {
		return aml.Job{}, false, apperror.NewValidation("invalid AML job lease")
	}
	txm := postgres.MustGetTxManager(ctx)
	var job aml.Job
	claimed := false
	err := txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		// A process may have died after the paid submit was accepted. Its old
		// lease can only become submission_unknown, never pending_submit.
		_, err := q.Exec(txCtx, `WITH expired AS (
			SELECT id FROM reg_crypto_aml_jobs WHERE status IN ('submitting','polling') AND lease_expires_at <= $1
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 100
		) UPDATE reg_crypto_aml_jobs j SET
			status=CASE WHEN status='submitting' THEN 'submission_unknown' ELSE 'awaiting_result' END,
			lease_token=NULL,lease_expires_at=NULL,last_error_code='lease_expired'
			FROM expired e WHERE j.id=e.id`, now)
		if err != nil {
			return fmt.Errorf("recover expired AML lease: %w", err)
		}
		_, err = q.Exec(txCtx, `UPDATE reg_crypto_aml_jobs SET status='needs_attention',last_error_code='deadline_expired'
			WHERE status IN ('pending_submit','awaiting_result') AND deadline_at <= $1`, now)
		if err != nil {
			return fmt.Errorf("expire AML job deadline: %w", err)
		}
		var payload, ticket []byte
		leaseToken := id.New()
		err = q.QueryRow(txCtx, `WITH candidate AS (
			SELECT j.id FROM reg_crypto_aml_jobs j WHERE j.status IN ('pending_submit','awaiting_result')
            AND j.next_attempt_at <= $1 AND j.deadline_at > $1 AND j._deleted_at IS NULL
            AND (j.status='awaiting_result' OR EXISTS(
                SELECT 1 FROM sys_crypto_aml_account_policies p JOIN cat_integration_accounts a ON a.id=p.account_id
                WHERE p.account_id=j.provider_account_id AND p.allow_submissions
                  AND a.provider=j.provider AND a.is_active AND NOT a.deletion_mark AND a._deleted_at IS NULL
                  AND (j.provider='testnet_aml' OR (j.provider='getblock_net' AND COALESCE((SELECT SUM(units) FROM reg_crypto_aml_package_credits WHERE account_id=a.id),0)>
                      (SELECT COUNT(*) FROM reg_crypto_aml_paid_admissions WHERE account_id=a.id)))
                  AND EXISTS(SELECT 1 FROM reg_crypto_aml_subjects s JOIN sys_crypto_aml_provider_mappings m
                      ON m.network_id=s.network_id AND m.token_id=s.token_id AND m.version=j.mapping_version AND m.capability_accepted
                      WHERE s.id=j.subject_id)
            ))
            ORDER BY j.next_attempt_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1
		) UPDATE reg_crypto_aml_jobs j SET
			status=CASE WHEN j.status='pending_submit' THEN 'submitting' ELSE 'polling' END,
			poll_request_id=CASE WHEN j.status='awaiting_result' THEN $4 ELSE j.poll_request_id END,
			lease_token=$2,lease_expires_at=$3,attempts=j.attempts+1
			FROM candidate c WHERE j.id=c.id
			RETURNING j.id,j.subject_id,j.generation,j.provider,j.provider_account_id,j.request_payload,
			j.request_digest,j.mapping_version,j.submit_request_id,COALESCE(j.poll_request_id,''),j.ticket,
			j.status,j.lease_token,j.lease_expires_at,j.deadline_at`, now, leaseToken, now.Add(lease), id.Compact(id.New())).Scan(
			&job.ID, &job.SubjectID, &job.Generation, &job.Provider, &job.ProviderAccountID, &payload, &job.RequestDigest,
			&job.MappingVersion, &job.SubmitRequestID, &job.PollRequestID, &ticket, &job.Status, &job.LeaseToken, &job.LeaseExpiresAt, &job.DeadlineAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("claim AML job: %w", err)
		}
		if err := json.Unmarshal(payload, &job.Request); err != nil {
			return fmt.Errorf("decode durable AML request: %w", err)
		}
		if len(ticket) > 0 {
			var decoded aml.Ticket
			if err := json.Unmarshal(ticket, &decoded); err != nil {
				return fmt.Errorf("decode durable AML ticket: %w", err)
			}
			job.Ticket = &decoded
		}
		claimed = true
		return nil
	})
	return job, claimed, err
}

func (r *AMLRepository) SaveAMLTicket(ctx context.Context, job aml.Job, ticket aml.Ticket, next time.Time) error {
	if err := job.ValidateTicket(ticket); err != nil {
		return err
	}
	payload, err := json.Marshal(ticket)
	if err != nil {
		return fmt.Errorf("encode AML ticket: %w", err)
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		tag, err := q.Exec(txCtx, `UPDATE reg_crypto_aml_jobs SET ticket=$3::jsonb,status='awaiting_result',
			next_attempt_at=$4,lease_token=NULL,lease_expires_at=NULL,last_error_code=''
			WHERE id=$1 AND lease_token=$2 AND status='submitting' AND lease_expires_at>NOW()`, job.ID, job.LeaseToken, string(payload), next)
		if err != nil {
			return fmt.Errorf("persist AML ticket: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("AML submit lease is stale")
		}
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_aml_provider_checks
			(subject_id,job_id,provider,provider_account_id,check_hash,request_digest,ticket)
			VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, job.SubjectID, job.ID, ticket.Provider, ticket.ProviderAccountID, ticket.CheckHash, ticket.RequestDigest, string(payload))
		if err != nil {
			return fmt.Errorf("persist AML provider check identity: %w", err)
		}
		return nil
	})
}

func (r *AMLRepository) SaveAMLReport(ctx context.Context, job aml.Job, report aml.Report) error {
	if job.Ticket == nil || report.ID == id.Nil() || report.Score == nil || !report.Score.Valid() || report.Provider != job.Provider ||
		report.ProviderAccountID != job.ProviderAccountID || report.CheckHash != job.Ticket.CheckHash || report.RequestDigest != job.RequestDigest ||
		report.MappingVersion != job.MappingVersion || report.CompletedAt.IsZero() || report.ReceivedAt.IsZero() || len(report.RawResponse) == 0 ||
		report.ContractVersion == "" || report.AdapterVersion == "" {
		return apperror.NewValidation("AML report does not match durable ticket")
	}
	signals, err := json.Marshal(report.Signals)
	if err != nil {
		return fmt.Errorf("encode AML signals: %w", err)
	}
	txm := postgres.MustGetTxManager(ctx)
	return txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		q := txm.GetQuerier(txCtx)
		tag, err := q.Exec(txCtx, `UPDATE reg_crypto_aml_jobs SET status='succeeded',lease_token=NULL,lease_expires_at=NULL,last_error_code=''
			WHERE id=$1 AND lease_token=$2 AND status='polling' AND lease_expires_at>NOW()`, job.ID, job.LeaseToken)
		if err != nil {
			return fmt.Errorf("complete AML poll: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("AML poll lease is stale")
		}
		tag, err = q.Exec(txCtx, `INSERT INTO reg_crypto_aml_reports
			(id,subject_id,provider_check_id,score_bp,coverage_accepted,contract_version,adapter_version,mapping_version,
			 request_digest,signals,raw_response,completed_at,received_at,dataset_as_of)
			SELECT $1,$2,c.id,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14
			FROM reg_crypto_aml_provider_checks c WHERE c.job_id=$3 AND c.subject_id=$2`,
			report.ID, job.SubjectID, job.ID, int(*report.Score), report.CoverageAccepted, report.ContractVersion, report.AdapterVersion,
			report.MappingVersion, report.RequestDigest, string(signals), report.RawResponse, report.CompletedAt, report.ReceivedAt, report.DatasetAsOf)
		if err != nil {
			return fmt.Errorf("persist immutable AML report: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return apperror.NewConflict("AML provider check identity is missing")
		}
		if err := r.invalidateAMLSubject(txCtx, job.SubjectID, "report_received"); err != nil {
			return err
		}
		_, err = q.Exec(txCtx, `INSERT INTO reg_crypto_aml_decision_queue(report_id,policy_version)
            SELECT $1,scope.policy_version FROM sys_crypto_aml_routing_scopes scope JOIN reg_crypto_aml_subjects subject
            ON subject.merchant_id=scope.merchant_id AND subject.network_id=scope.network_id AND subject.token_id=scope.token_id
            WHERE subject.id=$2 AND scope.enabled ON CONFLICT DO NOTHING`, report.ID, job.SubjectID)
		return err
	})
}

func (r *AMLRepository) RescheduleAMLPoll(ctx context.Context, job aml.Job, next time.Time) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_aml_jobs SET
		status='awaiting_result',next_attempt_at=$3,lease_token=NULL,lease_expires_at=NULL
		WHERE id=$1 AND lease_token=$2 AND status='polling' AND lease_expires_at>NOW()`, job.ID, job.LeaseToken, next)
	if err != nil {
		return fmt.Errorf("reschedule AML poll: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewConflict("AML poll lease is stale")
	}
	return nil
}

func (r *AMLRepository) MarkAMLAttention(ctx context.Context, job aml.Job, status aml.JobStatus, reason string) error {
	if (status != aml.JobNeedsAttention && status != aml.JobSubmissionUnknown) || reason == "" || len(reason) > 100 {
		return apperror.NewValidation("invalid AML job attention state")
	}
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_aml_jobs SET
		status=$3,last_error_code=$4,lease_token=NULL,lease_expires_at=NULL
		WHERE id=$1 AND lease_token=$2 AND status IN ('submitting','polling') AND lease_expires_at>NOW()`, job.ID, job.LeaseToken, string(status), reason)
	if err != nil {
		return fmt.Errorf("mark AML job attention: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewConflict("AML job lease is stale")
	}
	return nil
}

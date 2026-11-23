package crypto_repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto/aml"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

func (r *AMLRepository) ApplyNextAMLDecision(ctx context.Context, now time.Time) (bool, error) {
	if now.IsZero() {
		return false, apperror.NewValidation("AML assessment time is required")
	}
	applied := false
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var isolation string
		if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
			return err
		}
		if isolation != "read committed" {
			return apperror.NewValidation("AML decisions require read committed isolation")
		}
		var report aml.Report
		var policy aml.Policy
		var subjectID id.ID
		var generation int64
		var maxAge, ttl int
		var signals []byte
		// Lock scope first, consistently with receipt invalidation and projection.
		// No stale low decision can replace a newer generation's pending state.
		err := q.QueryRow(ctx, `SELECT s.id,j.generation,r.id,c.provider,c.provider_account_id,c.check_hash,r.request_digest,r.contract_version,r.adapter_version,r.mapping_version,
 r.score_bp,r.coverage_accepted,r.completed_at,r.received_at,r.dataset_as_of,r.raw_response,r.signals,
 p.version,p.low_max_bp,p.medium_max_bp,p.max_report_age_seconds,p.decision_ttl_seconds,p.accepted_signals,p.critical_signals
 FROM reg_crypto_aml_decision_queue pending JOIN reg_crypto_aml_reports r ON r.id=pending.report_id JOIN reg_crypto_aml_provider_checks c ON c.id=r.provider_check_id JOIN reg_crypto_aml_jobs j ON j.id=c.job_id
 JOIN reg_crypto_aml_subjects s ON s.id=r.subject_id
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=s.merchant_id AND scope.network_id=s.network_id AND scope.token_id=s.token_id AND scope.enabled
 JOIN sys_crypto_aml_risk_policies p ON p.version=scope.policy_version
 WHERE pending.policy_version=p.version
 ORDER BY pending.report_id,pending.policy_version FOR UPDATE OF scope SKIP LOCKED LIMIT 1`).Scan(&subjectID, &generation, &report.ID, &report.Provider, &report.ProviderAccountID, &report.CheckHash, &report.RequestDigest, &report.ContractVersion, &report.AdapterVersion, &report.MappingVersion,
			&report.Score, &report.CoverageAccepted, &report.CompletedAt, &report.ReceivedAt, &report.DatasetAsOf, &report.RawResponse, &signals, &policy.Version, &policy.LowMax, &policy.MediumMax, &maxAge, &ttl, &policy.AcceptedSignals, &policy.CriticalSignals)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("select AML decision evidence: %w", err)
		}
		if err := json.Unmarshal(signals, &report.Signals); err != nil {
			return err
		}
		policy.MaxReportAge = time.Duration(maxAge) * time.Second
		policy.DecisionTTL = time.Duration(ttl) * time.Second
		var savedClass aml.Class
		var savedReason string
		var savedExpiry *time.Time
		var evaluatedAt time.Time
		err = q.QueryRow(ctx, `SELECT risk_class,reason,expires_at,evaluated_at FROM reg_crypto_aml_decisions
 WHERE subject_id=$1 AND generation=$2 AND report_id=$3 AND policy_version=$4`, subjectID, generation, report.ID, policy.Version).Scan(&savedClass, &savedReason, &savedExpiry, &evaluatedAt)
		if err == nil {
			// Time is an assessment input. A replay is assessed at its recorded
			// clock, never later, so its TTL is never extended.
			assessment, err := policy.Assess(report, evaluatedAt)
			if err != nil {
				return err
			}
			if savedClass != assessment.Class || savedReason != assessment.Reason || (savedExpiry == nil) != assessment.ExpiresAt.IsZero() || (savedExpiry != nil && !savedExpiry.Equal(assessment.ExpiresAt)) {
				return apperror.NewConflict("AML decision differs from its original assessment")
			}
			_, err = q.Exec(ctx, `DELETE FROM reg_crypto_aml_decision_queue WHERE report_id=$1 AND policy_version=$2`, report.ID, policy.Version)
			applied = err == nil
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// PostgreSQL timestamps retain microseconds. Assess using exactly the
		// clock that will be retained, so replay does not lose sub-microseconds.
		now = now.UTC().Truncate(time.Microsecond)
		assessment, err := policy.Assess(report, now)
		if err != nil {
			return err
		}
		var expires *time.Time
		if !assessment.ExpiresAt.IsZero() {
			expires = &assessment.ExpiresAt
		}
		tag, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_decisions(subject_id,report_id,generation,policy_version,risk_class,reason,expires_at,evaluated_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(subject_id,generation,report_id,policy_version) DO NOTHING`, subjectID, report.ID, generation, policy.Version, assessment.Class, assessment.Reason, expires, now)
		if err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `DELETE FROM reg_crypto_aml_decision_queue WHERE report_id=$1 AND policy_version=$2`, report.ID, policy.Version); err != nil {
			return err
		}
		applied = true
		if tag.RowsAffected() == 0 {
			return nil
		}
		return r.invalidateAMLSubject(ctx, subjectID, "decision_recorded")
	})
	return applied, err
}

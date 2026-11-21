package crypto_repo

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/infrastructure/storage/postgres"
)

// RequestAMLRescreen is an explicit control-plane command, never an automatic
// paid retry. It creates a new generation and invalidates old low permissions
// in the same transaction. Historical high facts remain restrictive.
func (r *AMLRepository) RequestAMLRescreen(ctx context.Context, effectID id.ID, expected int64, reason, actor string) (id.ID, error) {
	if expected <= 0 || expected == math.MaxInt64 || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 || strings.TrimSpace(actor) == "" {
		return id.ID{}, apperror.NewValidation("rescreen generation, reason and actor are required")
	}
	var result id.ID
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		var scope amlScope
		var subjectID, walletID id.ID
		if err := q.QueryRow(ctx, `SELECT scope.merchant_id,scope.network_id,scope.token_id,scope.provider_account_id,scope.mapping_version,scope.job_deadline_seconds,c.subject_id,e.wallet_id
 FROM reg_crypto_aml_effects e JOIN reg_crypto_aml_effect_checks c ON c.effect_id=e.id
 JOIN sys_crypto_aml_routing_scopes scope ON scope.merchant_id=e.merchant_id AND scope.network_id=e.network_id AND scope.token_id=e.token_id AND scope.enabled
 WHERE e.id=$1 FOR UPDATE OF scope`, effectID).Scan(&scope.MerchantID, &scope.NetworkID, &scope.TokenID, &scope.AccountID, &scope.MappingVersion, &scope.DeadlineSeconds, &subjectID, &walletID); err != nil {
			return err
		}
		var generation int64
		var mapping string
		var payload []byte
		if err := q.QueryRow(ctx, `SELECT generation,mapping_version,request_payload FROM reg_crypto_aml_subjects WHERE id=$1 FOR UPDATE`, subjectID).Scan(&generation, &mapping, &payload); err != nil {
			return err
		}
		if generation != expected {
			return apperror.NewConflict("AML rescreen generation changed")
		}
		if mapping != scope.MappingVersion {
			return apperror.NewValidation("AML rescreen requires explicit mapping migration")
		}
		var request aml.CheckRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `UPDATE reg_crypto_aml_subjects SET generation=generation+1 WHERE id=$1`, subjectID); err != nil {
			return err
		}
		jobID, err := r.EnqueueAMLCheck(ctx, EnqueueAMLInput{WalletID: walletID, ProviderAccountID: scope.AccountID, MappingVersion: mapping, Request: request, DeadlineAt: time.Now().Add(time.Duration(scope.DeadlineSeconds) * time.Second)})
		if err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_aml_rescreen_requests(job_id,effect_id,previous_generation,generation,reason,requested_by) VALUES($1,$2,$3,$4,$5,$6)`, jobID, effectID, generation, generation+1, reason, actor); err != nil {
			return fmt.Errorf("record AML rescreen audit: %w", err)
		}
		result = jobID
		return r.invalidateAMLSubject(ctx, subjectID, "rescreen_requested")
	})
	return result, err
}

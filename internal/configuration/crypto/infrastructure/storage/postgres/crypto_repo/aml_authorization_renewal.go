package crypto_repo

import (
	"context"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"strings"
)

// AuthorizeExactSweepRenewal is an operator command, never an automatic retry.
// The immutable original authorization/envelope/nonce remains unchanged; a
// separate one-use permission allows a fresh control signature for those bytes.
func (r *AMLRepository) AuthorizeExactSweepRenewal(ctx context.Context, authorization id.ID, reason, actor string) (id.ID, error) {
	if id.IsNil(authorization) || len(strings.TrimSpace(reason)) < 3 || len(reason) > 500 || strings.TrimSpace(actor) == "" || len(actor) > 100 {
		return id.Nil(), apperror.NewValidation("exact renewal requires authorization, reason and operator")
	}
	renewal := id.New()
	err := postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
		_, err := q.Exec(ctx, `SELECT s.version FROM reg_crypto_signing_authorizations a JOIN reg_crypto_aml_internal_allocations p ON p.sweep_attempt_id=a.basis_id AND a.basis_type='CryptoSweepAttempt' JOIN sys_crypto_aml_routing_scopes s ON s.merchant_id=p.merchant_id AND s.network_id=p.network_id WHERE a.id=$1 ORDER BY s.merchant_id,s.network_id,s.token_id FOR UPDATE OF s`, authorization)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `INSERT INTO reg_crypto_aml_authorization_renewals(id,authorization_id,expires_at,reason,authorized_by) VALUES($1,$2,NOW()+interval '10 minutes',$3,$4)`, renewal, authorization, reason, actor)
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, `UPDATE reg_crypto_sweep_attempts attempt SET expires_at=GREATEST(attempt.expires_at,r.expires_at),updated_at=NOW()-interval '2 minutes' FROM reg_crypto_aml_authorization_renewals r JOIN reg_crypto_signing_authorizations a ON a.id=r.authorization_id WHERE r.id=$1 AND attempt.id=a.basis_id`, renewal)
		if err != nil {
			return err
		}
		return postgres.NewEventLogRepo().Write(ctx, eventlog.Event{
			Category: eventlog.CategorySystem, Severity: eventlog.SeverityInfo,
			EventType: "crypto.aml.exact_authorization_renewal", Source: "aml-control", UserID: actor,
			EntityType: "SigningAuthorization", EntityID: &authorization, Message: reason,
			Details: map[string]any{"renewalId": renewal.String(), "nonceChanged": false, "envelopeChanged": false},
		})
	})
	return renewal, err
}

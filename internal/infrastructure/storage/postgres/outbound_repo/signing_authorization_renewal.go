package outbound_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

func (r *OutboundRepo) applyExactAuthorizationRenewal(ctx context.Context, record SigningAuthorizationRecord) (SigningAuthorizationRecord, error) {
	if record.SignedTx != nil {
		return record, nil
	}
	var renewal id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT id,expires_at,authorization_jws FROM reg_crypto_aml_authorization_renewals WHERE authorization_id=$1 AND expires_at>NOW()`, record.ID).Scan(&renewal, &record.Authorization.ExpiresAt, &record.JWS)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, nil
	}
	if err != nil {
		return record, err
	}
	record.RenewalID = &renewal
	return record, nil
}

func (r *OutboundRepo) setRenewedAuthorizationJWS(ctx context.Context, renewal id.ID, jws string) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_aml_authorization_renewals SET authorization_jws=$2 WHERE id=$1 AND expires_at>NOW() AND (authorization_jws='' OR authorization_jws=$2)`, renewal, jws)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("exact authorization renewal expired or changed")
	}
	return nil
}

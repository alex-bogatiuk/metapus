package crypto_repo

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

func checkWithdrawalCancellationReplay(ctx context.Context, request id.ID, reason, actor string) error {
	var conflict bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM reg_crypto_aml_withdrawal_lines l
		LEFT JOIN reg_crypto_aml_withdrawal_cancellations c ON c.line_id=l.id
		WHERE l.request_id=$1 AND l.status IN ('reserved','held','cancelled')
		AND (c.line_id IS NULL OR c.reason<>$2 OR c.actor<>$3 OR c._deleted_at IS NOT NULL))`, request, reason, actor).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return apperror.NewConflict("withdrawal cancellation replay differs from its original reason or actor")
	}
	return nil
}

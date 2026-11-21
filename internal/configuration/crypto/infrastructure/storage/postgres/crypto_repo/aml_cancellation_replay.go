package crypto_repo

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

// Tables/keys are internal constants, never supplied by an API caller.
func recordAMLCancellation(ctx context.Context, resource bool, key id.ID, reason, actor string, insert bool) error {
	table, column := "reg_crypto_aml_allocation_cancellations", "allocation_id"
	if resource {
		table, column = "reg_crypto_aml_resource_cancellations", "budget_id"
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if insert {
		if _, err := q.Exec(ctx, `INSERT INTO `+table+`(`+column+`,reason,cancelled_by) VALUES($1,$2,$3) ON CONFLICT(`+column+`) DO NOTHING`, key, reason, actor); err != nil {
			return err
		}
	}
	var matches bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE `+column+`=$1 AND reason=$2 AND cancelled_by=$3 AND _deleted_at IS NULL)`, key, reason, actor).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return apperror.NewConflict("AML cancellation replay differs from its original reason or actor")
	}
	return nil
}

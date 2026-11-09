package crypto_repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

func outflowDecisionError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return crypto.ErrExternalOutflowBusy
	}
	return err
}

func (*ExternalOutflowWorkflowRepo) ReplayClassification(ctx context.Context, outflow *crypto.ExternalOutflow, command cryptoapp.ClassifyExternalOutflowCommand, actor id.ID) (bool, error) {
	if outflow.Status == crypto.ExternalOutflowPendingReview {
		return false, nil
	}
	evidence, err := json.Marshal(command.Evidence)
	if err != nil {
		return false, err
	}
	var exact bool
	err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT COALESCE(classification_basis->'command'=
 fn_crypto_outflow_decision_command('classify',id,$2,$3,$4,$5,$6,$7::jsonb),FALSE) FROM reg_crypto_external_outflows WHERE id=$1`,
		outflow.ID, actor, command.Reason, command.Classification, command.MatchedBasisType, command.MatchedBasisID, string(evidence)).Scan(&exact)
	return exact, err
}

func (*ExternalOutflowWorkflowRepo) ReplayResolution(ctx context.Context, outflow *crypto.ExternalOutflow, actor id.ID, reason string) (bool, error) {
	if outflow.Status != crypto.ExternalOutflowResolved {
		return false, nil
	}
	var exact bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT COALESCE(resolution_basis->'command'=
 fn_crypto_outflow_decision_command('resolve',id,$2,$3),FALSE) FROM reg_crypto_external_outflows WHERE id=$1`, outflow.ID, actor, reason).Scan(&exact)
	if err != nil {
		return false, err
	}
	if !exact {
		return false, apperror.NewConflict("outflow resolution replay differs from its saved decision or its original basis is unknown")
	}
	return true, nil
}

func (r *ExternalOutflowWorkflowRepo) ReplayCustodyDecision(ctx context.Context, document, actor id.ID, reason, status string) (*crypto.CryptoCustodyAdjustment, error) {
	if err := r.lockCustodyAdjustment(ctx, document, false); err != nil {
		return nil, err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var a crypto.CryptoCustodyAdjustment
	var exact bool
	kind := "reject"
	if status == "posted" {
		kind = "approve"
	}
	err := q.QueryRow(ctx, `SELECT id,external_outflow_id,network_id,wallet_id,token_id,amount,status,maker_id,decision_by,decision_reason,posted_version,created_at,decided_at,accounting_model_version,
 COALESCE(decision_basis->'command'=fn_crypto_outflow_decision_command($2,id,$3,$4),FALSE) FROM doc_crypto_custody_adjustments WHERE id=$1 FOR UPDATE`, document, kind, actor, reason).Scan(
		&a.ID, &a.ExternalOutflowID, &a.NetworkID, &a.WalletID, &a.TokenID, &a.Amount, &a.Status, &a.MakerID, &a.DecisionBy, &a.DecisionReason, &a.PostedVersion, &a.CreatedAt, &a.DecidedAt, &a.AccountingModelVersion, &exact)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperror.NewNotFound("custody adjustment", document.String())
	}
	if err != nil {
		return nil, outflowDecisionError(err)
	}
	if a.Status == "pending_approval" {
		return nil, nil
	}
	if a.Status != status || !exact {
		return nil, apperror.NewConflict("custody adjustment replay differs from its saved decision or its original basis is unknown")
	}
	if status == "posted" && a.AccountingModelVersion == 2 {
		if err := q.QueryRow(ctx, `SELECT period FROM reg_crypto_custody_loss_bindings WHERE adjustment_id=$1`, document).Scan(&a.RecognitionPeriod); err != nil {
			return nil, err
		}
	}
	return &a, nil
}

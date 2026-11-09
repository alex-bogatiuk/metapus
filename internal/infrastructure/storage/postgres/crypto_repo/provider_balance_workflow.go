package crypto_repo

import (
	"context"

	"github.com/jackc/pgx/v5"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type ProviderBalanceWorkflowRepo struct{}

func NewProviderBalanceWorkflowRepo() *ProviderBalanceWorkflowRepo {
	return &ProviderBalanceWorkflowRepo{}
}

func (*ProviderBalanceWorkflowRepo) LockAccount(ctx context.Context, accountID id.ID) error {
	var locked id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT id FROM cat_crypto_provider_accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&locked)
	if err == pgx.ErrNoRows {
		return apperror.NewNotFound("provider account", accountID)
	}
	return err
}

func (r *ProviderBalanceWorkflowRepo) LoadOpeningEvidence(ctx context.Context, incidentID, observationID id.ID) (cryptoapp.ProviderBalanceOpeningEvidence, error) {
	var evidence cryptoapp.ProviderBalanceOpeningEvidence
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var accountID id.ID
	err := q.QueryRow(ctx, `SELECT provider_account_id FROM reg_crypto_provider_balance_incidents WHERE id=$1`, incidentID).Scan(&accountID)
	if err == pgx.ErrNoRows {
		return evidence, apperror.NewNotFound("provider balance incident", incidentID)
	}
	if err != nil {
		return evidence, err
	}
	if err = r.LockAccount(ctx, accountID); err != nil {
		return evidence, err
	}
	err = q.QueryRow(ctx, `
 SELECT i.provider_account_id,i.token_id,i.status,obs.ledger_amount,obs.provider_amount,obs.delta_amount,obs.id,obs.observed_at
 FROM reg_crypto_provider_balance_incidents i
 JOIN cat_crypto_provider_accounts a ON a.id=i.provider_account_id AND a.balance_token_id=i.token_id AND a.is_active AND NOT a.deletion_mark
 JOIN reg_crypto_provider_balance_observations obs ON obs.id=$2 AND obs.provider_account_id=i.provider_account_id AND obs.token_id=i.token_id
 WHERE i.id=$1 AND i.status IN ('open','acknowledged') AND obs.delta_amount>0
 AND (i.ledger_amount,i.provider_amount,i.delta_amount)=(obs.ledger_amount,obs.provider_amount,obs.delta_amount)
 AND obs.id=(SELECT id FROM reg_crypto_provider_balance_observations WHERE provider_account_id=i.provider_account_id AND token_id=i.token_id ORDER BY observed_at DESC,id DESC LIMIT 1)
 AND obs.ledger_amount=(SELECT COALESCE(SUM(CASE WHEN record_type='receipt' THEN amount ELSE -amount END),0) FROM reg_crypto_provider_balance_movements WHERE provider_account_id=i.provider_account_id AND token_id=i.token_id)
 FOR UPDATE OF i`, incidentID, observationID).Scan(&evidence.ProviderAccountID, &evidence.TokenID, &evidence.Status, &evidence.LedgerAmount, &evidence.ProviderAmount, &evidence.DeltaAmount, &evidence.ObservationID, &evidence.ObservedAt)
	if err == pgx.ErrNoRows {
		return evidence, apperror.NewValidation("opening evidence is stale or does not match the active incident and current ledger")
	}
	return evidence, err
}

func (*ProviderBalanceWorkflowRepo) FindActiveOpeningByAccountToken(ctx context.Context, accountID, tokenID id.ID) (*id.ID, error) {
	var operationID id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT id FROM doc_crypto_provider_balance_operations WHERE provider_account_id=$1 AND token_id=$2 AND operation_type='opening' AND status IN ('pending_approval','approved','posted') LIMIT 1`, accountID, tokenID).Scan(&operationID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &operationID, nil
}

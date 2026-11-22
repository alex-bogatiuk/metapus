package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

type BalanceReconciliationRepo struct{}

func NewBalanceReconciliationRepo() *BalanceReconciliationRepo { return &BalanceReconciliationRepo{} }

func (*BalanceReconciliationRepo) LockBalanceWallet(ctx context.Context, walletID id.ID) error {
	txm := postgres.MustGetTxManager(ctx)
	if txm.GetTx(ctx) == nil {
		return apperror.NewValidation("balance reconciliation requires a transaction")
	}
	var locked id.ID
	err := txm.GetQuerier(ctx).QueryRow(ctx, `SELECT id FROM cat_wallets WHERE id=$1 FOR UPDATE`, walletID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperror.NewNotFound("wallet", walletID)
	}
	return err
}

func (*BalanceReconciliationRepo) InsertBalanceCheck(ctx context.Context, o crypto.BalanceObservation) (bool, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	// Validate the persisted identities, not just caller-supplied UUIDs/kind.
	var valid bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM cat_wallets w JOIN cat_tokens t ON t.network_id=w.network_id
		WHERE w.id=$1 AND w.network_id=$2 AND t.id=$3
		AND CASE WHEN t.token_standard='native' THEN 'native_fee_balance_mismatch' ELSE 'token_balance_mismatch' END=$4)
	`, o.WalletID, o.NetworkID, o.TokenID, o.Kind).Scan(&valid); err != nil {
		return false, err
	}
	if !valid {
		return false, apperror.NewValidation("balance observation wallet, network, token or kind mismatch")
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO reg_crypto_reconciliation_checks(run_id,network_id,wallet_id,token_id,ledger_amount,chain_amount,delta_amount)
		VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (run_id,wallet_id,token_id) DO NOTHING
	`, o.RunID, o.NetworkID, o.WalletID, o.TokenID, o.LedgerAmount, o.ChainAmount, o.Delta())
	if err != nil {
		return false, fmt.Errorf("insert balance check: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	var same bool
	if err := q.QueryRow(ctx, `
		SELECT network_id=$4 AND ledger_amount=$5 AND chain_amount=$6 AND delta_amount=$7
		FROM reg_crypto_reconciliation_checks WHERE run_id=$1 AND wallet_id=$2 AND token_id=$3
	`, o.RunID, o.WalletID, o.TokenID, o.NetworkID, o.LedgerAmount, o.ChainAmount, o.Delta()).Scan(&same); err != nil {
		return false, err
	}
	if !same {
		return false, apperror.NewConflict("reconciliation run already has different balance evidence")
	}
	return false, nil
}

func (*BalanceReconciliationRepo) FindOpenBalanceIncident(ctx context.Context, o crypto.BalanceObservation) (id.ID, error) {
	var incidentID id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT id FROM reg_crypto_reconciliation_items
		WHERE network_id=$1 AND wallet_id=$2 AND token_id=$3 AND kind=$4
		AND status IN ('open','acknowledged') FOR UPDATE
	`, o.NetworkID, o.WalletID, o.TokenID, o.Kind).Scan(&incidentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return id.Nil(), nil
	}
	return incidentID, err
}

func (*BalanceReconciliationRepo) CreateBalanceIncident(ctx context.Context, o crypto.BalanceObservation) (id.ID, error) {
	var incidentID id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		INSERT INTO reg_crypto_reconciliation_items(run_id,last_run_id,network_id,wallet_id,token_id,kind,ledger_amount,chain_amount,delta_amount)
		VALUES($1,$1,$2,$3,$4,$5,$6,$7,$8) RETURNING id
	`, o.RunID, o.NetworkID, o.WalletID, o.TokenID, o.Kind, o.LedgerAmount, o.ChainAmount, o.Delta()).Scan(&incidentID)
	return incidentID, err
}

func (*BalanceReconciliationRepo) UpdateBalanceIncident(ctx context.Context, incidentID id.ID, o crypto.BalanceObservation, resolved bool) error {
	tag, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE reg_crypto_reconciliation_items SET last_run_id=$2,ledger_amount=$3,chain_amount=$4,delta_amount=$5,
		last_seen_at=NOW(),occurrence_count=occurrence_count+1,updated_at=NOW(),
		status=CASE WHEN $6 THEN 'resolved' ELSE status END,
		resolution_reason=CASE WHEN $6 THEN 'auto_reconciled' ELSE resolution_reason END,
		resolved_at=CASE WHEN $6 THEN NOW() ELSE resolved_at END
		WHERE id=$1 AND status IN ('open','acknowledged')
	`, incidentID, o.RunID, o.LedgerAmount, o.ChainAmount, o.Delta(), resolved)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return apperror.NewConflict("balance incident is no longer open")
	}
	return nil
}

func (*BalanceReconciliationRepo) AppendBalanceObservation(ctx context.Context, incidentID id.ID, o crypto.BalanceObservation) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		INSERT INTO reg_crypto_reconciliation_observations(run_id,incident_id,network_id,wallet_id,token_id,kind,ledger_amount,chain_amount,delta_amount)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, o.RunID, incidentID, o.NetworkID, o.WalletID, o.TokenID, o.Kind, o.LedgerAmount, o.ChainAmount, o.Delta())
	return err
}

func (*BalanceReconciliationRepo) HasBalanceIncidentHold(ctx context.Context, incidentID id.ID) (bool, error) {
	var held bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds
		WHERE owner_type='reconciliation_incident' AND owner_id=$1::text AND status='open')
	`, incidentID).Scan(&held)
	return held, err
}

func (*BalanceReconciliationRepo) InitialBalanceIncidentObservation(ctx context.Context, incidentID id.ID) (crypto.BalanceObservation, error) {
	var o crypto.BalanceObservation
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT o.run_id,o.network_id,o.wallet_id,o.token_id,o.kind,o.ledger_amount,o.chain_amount
 FROM reg_crypto_reconciliation_items i JOIN reg_crypto_reconciliation_observations o ON o.incident_id=i.id AND o.run_id=i.run_id
 AND o.network_id=i.network_id AND o.wallet_id=i.wallet_id AND o.token_id=i.token_id AND o.kind=i.kind WHERE i.id=$1`, incidentID).Scan(&o.RunID, &o.NetworkID, &o.WalletID, &o.TokenID, &o.Kind, &o.LedgerAmount, &o.ChainAmount)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, apperror.NewConflict("balance incident requires its original immutable observation")
	}
	return o, err
}

func (*BalanceReconciliationRepo) PendingWalletCheckReady(ctx context.Context, walletID id.ID) (bool, error) {
	var ready bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM reg_crypto_wallet_allocation_holds h
		WHERE h.wallet_id=$1 AND h.owner_type='wallet_reconciliation' AND h.owner_id=$1::text
		AND h.reason='chain_balance_check_pending' AND h.status='open')
		AND NOT EXISTS(SELECT 1 FROM reg_crypto_reconciliation_items
		WHERE wallet_id=$1 AND kind IN ('token_balance_mismatch','native_fee_balance_mismatch') AND status IN ('open','acknowledged'))
	`, walletID).Scan(&ready)
	return ready, err
}

var _ cryptoapp.BalanceReconciliationRepository = (*BalanceReconciliationRepo)(nil)

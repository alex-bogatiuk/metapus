package outbound_repo

import (
	"context"

	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/infrastructure/storage/postgres"
)

// Store a stable machine-readable reason separately from diagnostic details.
func (r *OutboundRepo) RecordWithdrawalRecovery(ctx context.Context, requestID id.ID, reason, detail string) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE doc_withdrawal_requests
		SET recovery_reason=$2,recovery_started_at=COALESCE(recovery_started_at,NOW()),rejection_reason=$3
		WHERE id=$1 AND status IN ('approved','signing','held')`, requestID, reason, detail)
	return err
}

// LockWithdrawalFailure serializes compensation with authorization publication.
// A created authorization is already evidence of a possible signing call;
// absence of a persisted signed response is not proof that Vault did not sign.
func (r *OutboundRepo) LockWithdrawalFailure(ctx context.Context, requestID id.ID) (cryptoapp.WithdrawalFailureState, error) {
	basis, err := prepareWithdrawalResources(ctx, requestID)
	if err != nil {
		return cryptoapp.WithdrawalFailureState{}, err
	}
	if _, err := basis.lockWallets(ctx, nil, false); err != nil {
		return cryptoapp.WithdrawalFailureState{}, err
	}
	if err := basis.lockRequest(ctx); err != nil {
		return cryptoapp.WithdrawalFailureState{}, err
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var state cryptoapp.WithdrawalFailureState
	if err := q.QueryRow(ctx, `SELECT status,posted_version,EXISTS (
		SELECT 1 FROM doc_crypto_withdrawals cw WHERE cw.id=wr.withdrawal_id
		AND (cw.tx_hash<>'' OR cw.status<>'created' OR cw.posted))
		FROM doc_withdrawal_requests wr WHERE id=$1 AND _deleted_at IS NULL FOR UPDATE`, requestID).
		Scan(&state.Status, &state.PostedVersion, &state.HasExecutionEffect); err != nil {
		return state, err
	}
	rows, err := q.Query(ctx, `SELECT id FROM reg_crypto_signing_authorizations WHERE basis_type=$1 AND basis_id=$2 AND purpose=$3 ORDER BY id FOR UPDATE`, withdrawal_request.DocumentType, requestID, crypto.SigningPurposeWithdrawal)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		state.HasSigningAttempt = true
	}
	return state, rows.Err()
}

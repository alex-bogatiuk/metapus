package crypto_repo

import (
	"context"
	"fmt"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
	"time"
)

type NativeFundingSettlementRepo struct{}

func (NativeFundingSettlementRepo) GetNativeFundingForUpdate(ctx context.Context, fundingID id.ID) (crypto.NativeFunding, error) {
	var record crypto.NativeFunding
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return record, apperror.NewValidation("native funding settlement requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	var valid bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doc_crypto_native_fundings f
		JOIN cat_wallets s ON s.id=f.source_wallet_id JOIN cat_wallets d ON d.id=f.destination_wallet_id
		WHERE f.id=$1 AND s.network_id=f.network_id AND d.network_id=f.network_id
		AND s.custody_merchant_id IS NOT DISTINCT FROM d.custody_merchant_id)`, fundingID).Scan(&valid); err != nil {
		return record, err
	}
	if !valid {
		return record, apperror.NewConflict("native funding wallets require the same network and custody owner")
	}
	if err := LockNativeFundingResources(ctx, fundingID); err != nil {
		return record, err
	}
	err := q.QueryRow(ctx, `
		SELECT id,basis_type,basis_id,attempt,network_id,native_token_id,source_wallet_id,destination_wallet_id,
		       amount_minor,fee_reservation_id,signing_authorization_id,unsigned_fingerprint,tx_hash,status,last_error
		FROM doc_crypto_native_fundings WHERE id=$1 FOR UPDATE
	`, fundingID).Scan(&record.ID, &record.BasisType, &record.BasisID, &record.Attempt, &record.NetworkID,
		&record.NativeTokenID, &record.SourceWalletID, &record.DestinationWalletID, &record.Amount,
		&record.FeeReservationID, &record.SigningAuthorizationID, &record.UnsignedFingerprint,
		&record.TxHash, &record.Status, &record.LastError)
	if err == nil && record.FeeReservationID != nil {
		// Fee settlement takes the reservation before balance rows as well.
		err = q.QueryRow(ctx, "SELECT id FROM reg_crypto_fee_reservations WHERE id=$1 FOR UPDATE", *record.FeeReservationID).Scan(new(id.ID))
	}
	return record, err
}

func nativeFundingPeriod(receipt crypto.ChainReceipt) time.Time {
	when := receipt.ObservedAt
	if when.IsZero() {
		when = time.Now().UTC()
	}
	return when
}

func (NativeFundingSettlementRepo) RecordNativeFundingPrincipal(ctx context.Context, funding crypto.NativeFunding, receipt crypto.ChainReceipt) error {
	when := nativeFundingPeriod(receipt)
	sourceLineID, destinationLineID := nativeFundingPrincipalLines(funding.ID)
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if _, err := q.Exec(ctx, `
			INSERT INTO reg_crypto_balance_movements (
				line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount
			) VALUES
				($1,$3,'CryptoNativeFunding',1,$4,'expense',$5,$7,$8),
				($2,$3,'CryptoNativeFunding',1,$4,'receipt',$6,$7,$8)
		`, sourceLineID, destinationLineID, funding.ID, when, funding.SourceWalletID,
		funding.DestinationWalletID, funding.NativeTokenID, funding.Amount); err != nil {
		return fmt.Errorf("record native funding principal movements: %w", err)
	}
	return nil
}

func (NativeFundingSettlementRepo) FinishNativeFunding(ctx context.Context, fundingID id.ID, status, reason string, receipt crypto.ChainReceipt) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	tag, err := q.Exec(ctx, `
			UPDATE doc_crypto_native_fundings
			SET status=$4,solidified_at=COALESCE(solidified_at,$2),
			    settled_at=CASE WHEN $4='settled' THEN NOW() ELSE settled_at END,
			    last_error=$5,claimed_by='',claimed_at=NULL,lease_expires_at=NULL
			WHERE id=$1 AND status IN ('broadcast','solidified') AND tx_hash=$3
		`, fundingID, receipt.ObservedAt, receipt.TxHash, status, reason)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("mark native funding settled: state mismatch")
	}
	return nil
}

func (NativeFundingSettlementRepo) RequestNativeFundingReconciliation(ctx context.Context, funding crypto.NativeFunding) error {
	return enqueueReconciliationRequests(ctx,
		reconciliationRequest{funding.NetworkID, funding.SourceWalletID, funding.NativeTokenID, "native_funding_source", funding.ID},
		reconciliationRequest{funding.NetworkID, funding.DestinationWalletID, funding.NativeTokenID, "native_funding_destination", funding.ID})
}

package outbound_repo

import (
	"context"
	"fmt"

	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

// claimNativeFundingConfirmations deliberately has no parent, wallet status,
// policy-enabled or signing-readiness filter. An accepted chain transfer must
// finish accounting even when new allocations/signing are disabled.
func claimNativeFundingConfirmations(ctx context.Context, owner string, limit int) (result []nativeFundingRecord, err error) {
	if limit <= 0 {
		return nil, nil
	}
	err = postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		var err error
		result, err = claimLockedNativeFundingConfirmations(ctx, owner, limit)
		return err
	})
	return result, err
}

func claimLockedNativeFundingConfirmations(ctx context.Context, owner string, limit int) ([]nativeFundingRecord, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `
		WITH candidates AS MATERIALIZED (
		    SELECT id FROM doc_crypto_native_fundings
		    WHERE status IN ('broadcast','solidified') AND tx_hash <> ''
		      AND next_attempt_at <= NOW()
		      AND (lease_expires_at IS NULL OR lease_expires_at <= NOW())
		    ORDER BY next_attempt_at,id
		), picked AS MATERIALIZED (
		    SELECT id FROM candidates WHERE fn_crypto_try_lock_funding_confirmation(id) LIMIT $2
		)
		UPDATE doc_crypto_native_fundings f
		SET claimed_by=$1,claimed_at=NOW(),lease_expires_at=NOW()+interval '2 minutes'
		FROM picked WHERE f.id=picked.id
		  AND f.status IN ('broadcast','solidified') AND f.tx_hash<>''
		  AND f.next_attempt_at<=NOW() AND (f.lease_expires_at IS NULL OR f.lease_expires_at<=NOW())
		RETURNING f.id,f.network_id,f.tx_hash,f.amount_minor
	`, owner, limit)
	if err != nil {
		return nil, fmt.Errorf("claim native funding receipts: %w", err)
	}
	defer rows.Close()
	var result []nativeFundingRecord
	for rows.Next() {
		var f nativeFundingRecord
		if err := rows.Scan(&f.ID, &f.NetworkID, &f.TxHash, &f.Amount); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func releaseNativeFundingConfirmation(ctx context.Context, fundingID id.ID, owner string) error {
	return mutateNativeFunding(ctx, fundingID, func(ctx context.Context) error {
		_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
			UPDATE doc_crypto_native_fundings
			SET claimed_by='',claimed_at=NULL,lease_expires_at=NULL,next_attempt_at=NOW()+interval '30 seconds'
			WHERE id=$1 AND claimed_by=$2
		`, fundingID, owner)
		return err
	})
}

type NativeFundingConfirmationStore struct{}

func (NativeFundingConfirmationStore) ClaimNativeFundingConfirmations(ctx context.Context, owner string, limit int) ([]nativeFundingRecord, error) {
	return claimNativeFundingConfirmations(ctx, owner, limit)
}

func (NativeFundingConfirmationStore) ReleaseNativeFundingConfirmation(ctx context.Context, funding id.ID, owner string) error {
	return releaseNativeFundingConfirmation(ctx, funding, owner)
}

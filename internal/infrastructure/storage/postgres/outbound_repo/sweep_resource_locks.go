package outbound_repo

import (
	"context"
	"sort"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/infrastructure/storage/postgres"
)

func lockSweepDraftResources(ctx context.Context, attempts []id.ID, candidate *crypto_sweep.CryptoSweep) error {
	ordered := append([]id.ID(nil), attempts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	locker := postgres.NewDocLocker()
	for i, attempt := range ordered {
		if i > 0 && attempt == ordered[i-1] {
			return apperror.NewValidation("sweep attempt selected twice")
		}
		if err := locker.LockDocument(ctx, "CryptoSweepAttempt", attempt); err != nil {
			return err
		}
	}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	rows, err := q.Query(ctx, `SELECT DISTINCT sweep_id FROM reg_crypto_sweep_attempts WHERE id=ANY($1) AND sweep_id IS NOT NULL ORDER BY sweep_id`, ordered)
	if err != nil {
		return err
	}
	var existing []id.ID
	for rows.Next() {
		var doc id.ID
		if err := rows.Scan(&doc); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, doc)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(existing) > 1 {
		return apperror.NewConflict("sweep attempts belong to different execution documents")
	}
	var document id.ID
	if len(existing) == 1 {
		document = existing[0]
	} else if candidate != nil {
		document = candidate.ID
	} else {
		return apperror.NewValidation("new sweep execution draft requires a candidate")
	}
	if err := locker.LockDocument(ctx, crypto_sweep.DocumentType, document); err != nil {
		return err
	}
	var proposed []id.ID
	if candidate != nil {
		proposed = append(proposed, candidate.HotWalletID)
		for _, line := range candidate.GetLines() {
			proposed = append(proposed, line.WalletID)
		}
	}
	_, err = q.Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
		SELECT wallet_id FROM reg_crypto_sweep_attempts WHERE id=ANY($1)
		UNION SELECT hot_wallet_id FROM reg_crypto_sweep_attempts WHERE id=ANY($1)
		UNION SELECT hot_wallet_id FROM doc_crypto_sweeps WHERE id=$2
		UNION SELECT wallet_id FROM doc_crypto_sweeps__lines WHERE document_id=$2
		UNION SELECT unnest($3::uuid[])))`, ordered, document, proposed)
	return err
}

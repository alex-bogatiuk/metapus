package outbound_repo

import (
	"context"
	"sort"

	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

func (r *OutboundRepo) PrepareSweepMaintenance(ctx context.Context) (cryptoapp.SweepMaintenanceBatch, error) {
	var batch cryptoapp.SweepMaintenanceBatch
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return batch, apperror.NewValidation("sweep maintenance requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	var isolation string
	if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return batch, err
	}
	if isolation != "read committed" {
		return batch, apperror.NewValidation("sweep maintenance requires read committed isolation")
	}
	err := q.QueryRow(ctx, `WITH
		auth AS MATERIALIZED (
		 SELECT id FROM reg_crypto_signing_authorizations s WHERE signed_envelope IS NULL
		 AND (status='failed' OR (status IN ('created','held') AND expires_at<=NOW()
		 AND NOT EXISTS(SELECT 1 FROM reg_crypto_aml_authorization_renewals r WHERE r.authorization_id=s.id AND r.expires_at>NOW())))
		), fees AS MATERIALIZED (
		 SELECT f.id FROM reg_crypto_fee_reservations f JOIN reg_crypto_signing_authorizations s
		 ON s.basis_type=f.basis_type AND s.basis_id=f.basis_id AND s.purpose=f.purpose AND s.attempt=f.attempt
		 WHERE s.id IN (SELECT id FROM auth) AND (f.status IN ('reserved','held') OR EXISTS(SELECT 1 FROM reg_crypto_aml_resource_budgets b WHERE b.fee_reservation_id=f.id AND b.status='reserved'))
		), budgets AS MATERIALIZED (
		 SELECT id FROM reg_crypto_aml_resource_budgets WHERE fee_reservation_id IN (SELECT id FROM fees) AND status='reserved'
		), attempts AS MATERIALIZED (
		 SELECT id FROM reg_crypto_sweep_attempts a WHERE (status='expired' AND EXISTS(SELECT 1 FROM reg_crypto_aml_internal_allocations p WHERE p.sweep_attempt_id=a.id AND p.status='reserved'))
		 OR (status IN ('claimed','resource_ready','signing','held') AND expires_at<=NOW())
		), allocations AS MATERIALIZED (
		 SELECT id FROM reg_crypto_aml_internal_allocations WHERE sweep_attempt_id IN (SELECT id FROM attempts) AND status='reserved'
		), drafts AS MATERIALIZED (
		 SELECT id FROM doc_crypto_sweeps d WHERE status IN ('created','held') AND NOT posted
		 AND EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.sweep_id=d.id)
		 AND NOT EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.sweep_id=d.id AND a.status NOT IN ('expired','failed') AND a.id NOT IN (SELECT id FROM attempts))
		), wallets AS MATERIALIZED (
		 SELECT id FROM cat_wallets w WHERE tier='pool' AND wallet_role='merchant_pool'
		 AND status IN ('sweeping','free','assigned') AND is_active AND NOT deletion_mark
		 AND EXISTS(SELECT 1 FROM reg_crypto_sweep_attempts a WHERE a.wallet_id=w.id AND (a.status IN ('expired','failed') OR a.id IN (SELECT id FROM attempts)))
		)
		SELECT ARRAY(SELECT id FROM auth),ARRAY(SELECT DISTINCT id FROM fees),ARRAY(SELECT id FROM budgets),
		 ARRAY(SELECT id FROM attempts),ARRAY(SELECT id FROM allocations),ARRAY(SELECT id FROM drafts),ARRAY(SELECT id FROM wallets)`).Scan(&batch.Authorizations, &batch.Fees, &batch.Budgets, &batch.Attempts, &batch.Allocations, &batch.Drafts, &batch.Wallets)
	if err != nil {
		return batch, err
	}
	// Draft linking uses attempt -> document advisories before resources.
	// Stabilize its mutable document/FK footprint in that same order.
	locker := postgres.NewDocLocker()
	for _, group := range []struct {
		kind string
		ids  []id.ID
	}{{_sweepAttemptBasisType, batch.Attempts}, {crypto_sweep.DocumentType, batch.Drafts}} {
		sort.Slice(group.ids, func(i, j int) bool { return group.ids[i].String() < group.ids[j].String() })
		for _, key := range group.ids {
			if err := locker.LockDocument(ctx, group.kind, key); err != nil {
				return batch, err
			}
		}
	}
	// Include every possible FK/queue/invalidation resource for the frozen
	// cohort, including wallets recovered only after expiry in this transaction.
	_, err = q.Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
		SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE id=ANY($1::uuid[])
		UNION SELECT wallet_id FROM reg_crypto_aml_resource_budgets WHERE id=ANY($2::uuid[])
		UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE fee_reservation_id=ANY($1::uuid[])
		UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE fee_reservation_id=ANY($1::uuid[])
		UNION SELECT wallet_id FROM reg_crypto_sweep_attempts WHERE id=ANY($3::uuid[])
		UNION SELECT hot_wallet_id FROM reg_crypto_sweep_attempts WHERE id=ANY($3::uuid[])
		UNION SELECT source_wallet_id FROM reg_crypto_aml_internal_allocations WHERE id=ANY($4::uuid[])
		UNION SELECT destination_wallet_id FROM reg_crypto_aml_internal_allocations WHERE id=ANY($4::uuid[])
		UNION SELECT hot_wallet_id FROM doc_crypto_sweeps WHERE id=ANY($5::uuid[])
		UNION SELECT wallet_id FROM doc_crypto_sweeps__lines WHERE document_id=ANY($5::uuid[])
		UNION SELECT unnest($6::uuid[])))`, batch.Fees, batch.Budgets, batch.Attempts, batch.Allocations, batch.Drafts, batch.Wallets)
	return batch, err
}

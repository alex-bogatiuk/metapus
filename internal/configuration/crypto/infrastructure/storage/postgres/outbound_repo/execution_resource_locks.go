package outbound_repo

import (
	"context"

	"metapus/internal/configuration/crypto/domain/documents/crypto_sweep"
	"metapus/internal/configuration/crypto/domain/documents/treasury_withdrawal"
	"metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

// This prefix also covers fee-manager calls later in the same publication
// transaction. Never update an execution row and only then acquire its scopes.
func lockExecutionResources(ctx context.Context, kind string, basis id.ID, extra []id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("execution resources require a transaction")
	}
	q := tm.GetQuerier(ctx)
	locker := postgres.NewDocLocker()
	switch kind {
	case withdrawal_request.DocumentType:
		request, err := prepareWithdrawalResources(ctx, basis)
		if err != nil {
			return err
		}
		if _, err := request.lockWalletSet(ctx, extra, false); err != nil {
			return err
		}
		return request.lockRequest(ctx)
	case "CryptoAMLWithdrawalLine":
		var merchant, network id.ID
		var execution *id.ID
		if err := q.QueryRow(ctx, `SELECT p.merchant_id,p.network_id,l.withdrawal_id FROM reg_crypto_aml_withdrawal_lines l
			JOIN reg_crypto_aml_withdrawal_plans p ON p.request_id=l.request_id WHERE l.id=$1`, basis).Scan(&merchant, &network, &execution); err != nil {
			return err
		}
		if execution != nil {
			if err := locker.LockDocument(ctx, "CryptoWithdrawal", *execution); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx, `SELECT fn_crypto_lock_withdrawal_wallets(ARRAY(
			SELECT wallet_id FROM reg_crypto_aml_withdrawal_lines WHERE id=$1
			UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1
			UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1 AND status NOT IN ('settled','failed')
			UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoAMLWithdrawalLine' AND basis_id=$1 AND status NOT IN ('settled','failed')
			UNION SELECT unnest($2::uuid[])),$3,$4)`, basis, extra, merchant, network)
		return err
	case _sweepAttemptBasisType:
		if err := locker.LockDocument(ctx, _sweepAttemptBasisType, basis); err != nil {
			return err
		}
		var document *id.ID
		if err := q.QueryRow(ctx, "SELECT sweep_id FROM reg_crypto_sweep_attempts WHERE id=$1", basis).Scan(&document); err != nil {
			return err
		}
		if document != nil {
			if err := locker.LockDocument(ctx, crypto_sweep.DocumentType, *document); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx, `WITH attempts AS MATERIALIZED (
			SELECT id FROM reg_crypto_sweep_attempts WHERE id=$1 OR sweep_id=$2)
			SELECT fn_crypto_lock_custody_wallets(ARRAY(
			SELECT wallet_id FROM reg_crypto_sweep_attempts WHERE id IN (SELECT id FROM attempts)
			UNION SELECT hot_wallet_id FROM reg_crypto_sweep_attempts WHERE id IN (SELECT id FROM attempts)
			UNION SELECT hot_wallet_id FROM doc_crypto_sweeps WHERE id=$2
			UNION SELECT wallet_id FROM doc_crypto_sweeps__lines WHERE document_id=$2
			UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE basis_type='CryptoSweepAttempt' AND basis_id IN (SELECT id FROM attempts)
			UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoSweepAttempt' AND basis_id IN (SELECT id FROM attempts) AND status NOT IN ('settled','failed')
			UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='CryptoSweepAttempt' AND basis_id IN (SELECT id FROM attempts) AND status NOT IN ('settled','failed')
			UNION SELECT unnest($3::uuid[])))`, basis, document, extra)
		return err
	case treasury_withdrawal.DocumentType:
		if err := locker.LockDocument(ctx, treasury_withdrawal.DocumentType, basis); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `SELECT fn_crypto_lock_custody_wallets(ARRAY(
			SELECT source_wallet_id FROM doc_treasury_withdrawals WHERE id=$1
			UNION SELECT wallet_id FROM reg_crypto_balance_reservations WHERE document_type='TreasuryWithdrawal' AND document_id=$1
			UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1
			UNION SELECT source_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1 AND status NOT IN ('settled','failed')
			UNION SELECT destination_wallet_id FROM doc_crypto_native_fundings WHERE basis_type='TreasuryWithdrawal' AND basis_id=$1 AND status NOT IN ('settled','failed')
			UNION SELECT unnest($2::uuid[])))`, basis, extra)
		return err
	case nativeFundingBasisType:
		return crypto_repo.LockNativeFundingResources(ctx, basis, extra...)
	default:
		// Other authorization profiles (including activation) do not update a
		// business document here; their SQL guards still need all address/FK
		// resources before taking allocation or budget rows.
		return crypto_repo.NewCustodyRecognitionRepo().LockWallets(ctx, extra...)
	}
}

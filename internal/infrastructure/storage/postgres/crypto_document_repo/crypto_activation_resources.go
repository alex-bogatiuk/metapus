package crypto_document_repo

import (
	"context"
	"slices"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/infrastructure/storage/postgres"
)

// CryptoActivationWalletSetSQL is also used by the nonblocking selector.
const CryptoActivationWalletSetSQL = `SELECT ARRAY(SELECT id FROM (
 SELECT wallet_id id FROM reg_crypto_account_activations WHERE id=$1
 UNION SELECT source_wallet_id FROM doc_crypto_network_operations WHERE id=$2
 UNION SELECT payer_wallet_id FROM reg_crypto_fee_reservations WHERE basis_type='CryptoAccountActivationTask' AND basis_id=$1
 UNION SELECT w.id FROM reg_crypto_signing_authorizations a JOIN cat_wallets w ON w.network_id=a.network_id AND (w.address=a.source_address OR w.address=a.fee_payer_address)
 WHERE a.basis_type='CryptoAccountActivationTask' AND a.basis_id=$1
 UNION SELECT unnest($3::uuid[])) resources WHERE id IS NOT NULL ORDER BY id)`

// LockCryptoActivationResources is the shared prefix for actor and document writers.
func LockCryptoActivationResources(ctx context.Context, key id.ID, reserving bool, extra ...id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("activation state requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	var isolation string
	if err := q.QueryRow(ctx, "SHOW transaction_isolation").Scan(&isolation); err != nil {
		return err
	}
	required := "read committed"
	if reserving {
		required = "serializable"
	}
	if isolation != required {
		if reserving {
			return apperror.NewValidation("activation fee reservation requires serializable isolation")
		}
		return apperror.NewValidation("activation state requires read committed isolation")
	}
	locker := postgres.NewDocLocker()
	if err := locker.LockDocument(ctx, crypto_network_operation.ActivationTaskBasisType, key); err != nil {
		return err
	}
	var network, target id.ID
	var operation *id.ID
	if err := q.QueryRow(ctx, `SELECT network_id,wallet_id,network_operation_id FROM reg_crypto_account_activations WHERE id=$1`, key).Scan(&network, &target, &operation); err != nil {
		return err
	}
	if operation != nil {
		if err := locker.LockDocument(ctx, crypto_network_operation.DocumentType, *operation); err != nil {
			return err
		}
	}
	if _, err := q.Exec(ctx, `SELECT network_id FROM sys_crypto_wallet_pool_policies WHERE network_id=$1 FOR UPDATE`, network); err != nil {
		return err
	}
	var wallets, current []id.ID
	if err := q.QueryRow(ctx, CryptoActivationWalletSetSQL, key, operation, extra).Scan(&wallets); err != nil {
		return err
	}
	if reserving {
		// FeeBudget uses scopes -> network policy -> payer wallet. Take the full
		// union in that same order before either activation or fee budget writes.
		if _, err := q.Exec(ctx, `SELECT fn_crypto_lock_custody_scopes($1::uuid[])`, wallets); err != nil {
			return err
		}
		var policy id.ID
		if err := q.QueryRow(ctx, `SELECT network_id FROM sys_crypto_network_policies WHERE network_id=$1 AND _deleted_at IS NULL FOR UPDATE`, network).Scan(&policy); err != nil {
			return err
		}
	}
	if _, err := q.Exec(ctx, "SELECT fn_crypto_lock_custody_wallets($1::uuid[])", wallets); err != nil {
		return err
	}
	if err := q.QueryRow(ctx, CryptoActivationWalletSetSQL, key, operation, extra).Scan(&current); err != nil {
		return err
	}
	if !slices.Equal(wallets, current) {
		return apperror.NewConflict("activation resource footprint changed; retry the command")
	}
	var currentNetwork, currentTarget id.ID
	var currentOperation *id.ID
	if err := q.QueryRow(ctx, `SELECT network_id,wallet_id,network_operation_id FROM reg_crypto_account_activations WHERE id=$1 FOR UPDATE`, key).Scan(&currentNetwork, &currentTarget, &currentOperation); err != nil {
		return err
	}
	if network != currentNetwork || target != currentTarget || (operation == nil) != (currentOperation == nil) || (operation != nil && *operation != *currentOperation) {
		return apperror.NewConflict("activation resource identity changed")
	}
	return nil
}

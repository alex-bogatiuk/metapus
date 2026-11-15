package outbound_repo

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
)

const activationWalletSetSQL = crypto_document_repo.CryptoActivationWalletSetSQL

// Called after the common prefix has serialized task creation for this wallet.
// A delayed success from failed history must not hide a newer live workflow
// from the selector by marking its wallet active.
func ensureActivationNotSuperseded(ctx context.Context, key, walletID id.ID) error {
	var superseded bool
	if err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM reg_crypto_account_activations WHERE wallet_id=$2 AND id<>$1
 AND status IN ('claimed','signing','broadcast','confirmed','held'))`, key, walletID).Scan(&superseded); err != nil {
		return err
	}
	if superseded {
		return apperror.NewConflict("activation task has been superseded by another workflow")
	}
	return nil
}

// Shared operational prefix: workflow/operation advisories -> pool policy ->
// all known scopes/wallets -> activation row. Linked-operation completion and
// budget reservation must not acquire the policy after holding a wallet.
func lockActivationState(ctx context.Context, key id.ID, extra ...id.ID) error {
	return lockActivationResources(ctx, key, false, extra...)
}
func lockActivationResources(ctx context.Context, key id.ID, reserving bool, extra ...id.ID) error {
	return crypto_document_repo.LockCryptoActivationResources(ctx, key, reserving, extra...)
}

func (r *OutboundRepo) PrepareAccountActivationState(ctx context.Context, task AccountActivationTask) error {
	return r.prepareAccountActivationState(ctx, task, false)
}
func (r *OutboundRepo) prepareAccountActivationState(ctx context.Context, task AccountActivationTask, reserving bool) error {
	var valid bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_account_activations a JOIN cat_wallets target ON target.id=a.wallet_id
 JOIN cat_wallets source ON source.id=$4 AND source.network_id=a.network_id AND (
 source.custody_merchant_id IS NOT DISTINCT FROM target.custody_merchant_id
 OR EXISTS(SELECT 1 FROM doc_crypto_network_operations o WHERE o.id=a.network_operation_id AND o.source_wallet_id=source.id)
 OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations s WHERE s.basis_type='CryptoAccountActivationTask' AND s.basis_id=a.id AND s.source_address=source.address AND s.vault_key_id=source.vault_key_id))
 JOIN cat_tokens t ON t.id=$5 AND t.network_id=a.network_id AND t.token_standard='native'
 WHERE a.id=$1 AND a.network_id=$2 AND a.wallet_id=$3)`, task.ActivationID, task.NetworkID, task.WalletID, task.HotWallet.ID, task.CostTokenID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("activation task payer or asset identity changed")
	}
	if err := lockActivationResources(ctx, task.ActivationID, reserving, task.HotWallet.ID); err != nil {
		return err
	}
	err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM reg_crypto_account_activations a JOIN cat_wallets target ON target.id=a.wallet_id
 JOIN cat_wallets source ON source.id=$4 AND source.network_id=a.network_id AND (
 source.custody_merchant_id IS NOT DISTINCT FROM target.custody_merchant_id
 OR EXISTS(SELECT 1 FROM doc_crypto_network_operations o WHERE o.id=a.network_operation_id AND o.source_wallet_id=source.id)
 OR EXISTS(SELECT 1 FROM reg_crypto_signing_authorizations s WHERE s.basis_type='CryptoAccountActivationTask' AND s.basis_id=a.id AND s.source_address=source.address AND s.vault_key_id=source.vault_key_id))
 JOIN cat_tokens t ON t.id=$5 AND t.network_id=a.network_id AND t.token_standard='native'
 WHERE a.id=$1 AND a.network_id=$2 AND a.wallet_id=$3)`, task.ActivationID, task.NetworkID, task.WalletID, task.HotWallet.ID, task.CostTokenID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("activation task identity changed")
	}
	if err := validateActivationExecutionClaim(ctx, task); err != nil {
		return err
	}
	_, err = postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_account_activations SET activation_lease_expires_at=clock_timestamp()+($2*interval '1 second') WHERE id=$1`, task.ActivationID, int64(_accountActivationLease.Seconds()))
	return err
}

func mutateActivationState(ctx context.Context, key id.ID, mutate func(context.Context) error) error {
	return postgres.MustGetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := lockActivationState(ctx, key); err != nil {
			return err
		}
		return mutate(ctx)
	})
}

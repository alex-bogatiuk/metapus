package document_repo

import (
	"context"
	"slices"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/infrastructure/storage/postgres"
)

const networkOperationWalletsSQL = `SELECT ARRAY(SELECT wallet FROM (
 SELECT source_wallet_id wallet FROM doc_crypto_network_operations WHERE id=$1
 UNION SELECT target_wallet_id FROM doc_crypto_network_operations WHERE id=$1
 UNION SELECT wallet_id FROM reg_crypto_balance_movements WHERE recorder_id=$1 AND recorder_type='CryptoNetworkOperation'
 UNION SELECT unnest($2::uuid[])) resources WHERE wallet IS NOT NULL ORDER BY wallet)`

// LockResources precedes document rows and posting/reversal balances. Include
// proposed wallets as well as every historical posting wallet for corrections.
func (r *CryptoNetworkOperationRepo) LockResources(ctx context.Context, docID id.ID, extra ...id.ID) error {
	tm := r.getTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("network operation resources require a transaction")
	}
	doc, err := r.GetByID(ctx, docID)
	if err != nil {
		return err
	}
	if doc.BasisType != crypto_network_operation.ActivationTaskBasisType || doc.BasisID == nil {
		return apperror.NewConflict("network operation requires its activation workflow")
	}
	locker := postgres.NewDocLocker()
	if err := locker.LockDocument(ctx, doc.BasisType, *doc.BasisID); err != nil {
		return err
	}
	if err := locker.LockDocument(ctx, crypto_network_operation.DocumentType, docID); err != nil {
		return err
	}
	q := tm.GetQuerier(ctx)
	var wallets, current []id.ID
	if err := q.QueryRow(ctx, networkOperationWalletsSQL, docID, extra).Scan(&wallets); err != nil {
		return err
	}
	if err := postgres.LockCryptoActivationResources(ctx, *doc.BasisID, false, wallets...); err != nil {
		return err
	}
	if err := q.QueryRow(ctx, networkOperationWalletsSQL, docID, extra).Scan(&current); err != nil {
		return err
	}
	if !slices.Equal(wallets, current) {
		return apperror.NewConflict("network operation resource footprint changed; retry the command")
	}
	var valid bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM doc_crypto_network_operations d
 JOIN reg_crypto_account_activations a ON a.id=d.basis_id
 WHERE d.id=$1 AND d.basis_type=$2 AND d.basis_id=$3 AND d.network_id=$4 AND d.target_wallet_id=$5
 AND a.network_id=d.network_id AND a.wallet_id=d.target_wallet_id
 AND (a.network_operation_id IS NULL OR a.network_operation_id=d.id))`, docID, doc.BasisType, *doc.BasisID, doc.NetworkID, doc.TargetWalletID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return apperror.NewConflict("network operation workflow identity changed")
	}
	return nil
}

func (r *CryptoNetworkOperationRepo) GetByIDForUpdate(ctx context.Context, docID id.ID) (*crypto_network_operation.CryptoNetworkOperation, error) {
	if err := r.LockResources(ctx, docID); err != nil {
		return nil, err
	}
	return r.BaseDocumentRepo.GetByIDForUpdate(ctx, docID)
}

func (r *CryptoNetworkOperationRepo) Update(ctx context.Context, doc *crypto_network_operation.CryptoNetworkOperation) error {
	return r.getTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := r.LockResources(ctx, doc.ID, doc.SourceWalletID, doc.TargetWalletID); err != nil {
			return err
		}
		return r.BaseDocumentRepo.Update(ctx, doc)
	})
}

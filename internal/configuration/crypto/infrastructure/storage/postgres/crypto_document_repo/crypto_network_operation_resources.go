package crypto_document_repo

import (
	"context"

	"metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
)

// LockResources precedes the document row and the balances its posting moves:
// the activation workflow, the document, then the activation's wallets.
func (r *CryptoNetworkOperationRepo) LockResources(ctx context.Context, docID id.ID) error {
	tm := r.GetTxManager(ctx)
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
	// The activation footprint already holds this operation's payer and target.
	if err := LockCryptoActivationResources(ctx, *doc.BasisID, false); err != nil {
		return err
	}
	q := tm.GetQuerier(ctx)
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
	return r.GetTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := r.LockResources(ctx, doc.ID); err != nil {
			return err
		}
		return r.BaseDocumentRepo.Update(ctx, doc)
	})
}

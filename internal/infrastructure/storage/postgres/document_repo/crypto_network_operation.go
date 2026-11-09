package document_repo

import (
	"context"
	"fmt"

	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/infrastructure/storage/postgres"
)

type CryptoNetworkOperationRepo struct {
	*BaseDocumentRepo[*crypto_network_operation.CryptoNetworkOperation]
}

func NewCryptoNetworkOperationRepo() *CryptoNetworkOperationRepo {
	return &CryptoNetworkOperationRepo{BaseDocumentRepo: NewDocumentRepoForDefinition[*crypto_network_operation.CryptoNetworkOperation](crypto_network_operation.Definition,
		postgres.ExtractDBColumns[crypto_network_operation.CryptoNetworkOperation](),
		func() *crypto_network_operation.CryptoNetworkOperation {
			return &crypto_network_operation.CryptoNetworkOperation{}
		},
	)}
}

func (r *CryptoNetworkOperationRepo) SetManualEditAllowed(ctx context.Context, docID id.ID, allowed bool) error {
	return r.getTxManager(ctx).RunInTransaction(ctx, func(ctx context.Context) error {
		if err := r.LockResources(ctx, docID); err != nil {
			return err
		}
		return r.setManualEditAllowed(ctx, docID, allowed)
	})
}

func (r *CryptoNetworkOperationRepo) setManualEditAllowed(ctx context.Context, docID id.ID, allowed bool) error {
	tag, err := r.getTxManager(ctx).GetQuerier(ctx).Exec(ctx, `
		UPDATE doc_crypto_network_operations
		SET manual_edit_allowed=$2, version=version+1, updated_at=NOW()
		WHERE id=$1 AND manual_edit_allowed IS DISTINCT FROM $2
	`, docID, allowed)
	if err != nil {
		return fmt.Errorf("set crypto network operation protection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := r.getTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM doc_crypto_network_operations WHERE id=$1)`, docID,
		).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return apperror.NewNotFound("crypto_network_operation", docID.String())
		}
	}
	return nil
}

func (r *CryptoNetworkOperationRepo) ValidatePostingReferences(ctx context.Context, doc *crypto_network_operation.CryptoNetworkOperation) error {
	if doc.ChainSettlementID == nil {
		return apperror.NewBusinessRule("CRYPTO_NETWORK_OPERATION_SETTLEMENT_REQUIRED", "final chain settlement is required")
	}
	var valid bool
	err := r.getTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM cat_wallets target
			JOIN cat_wallets source ON source.id=$2
			JOIN cat_tokens token ON token.id=$3
			JOIN reg_crypto_chain_settlements settlement ON settlement.id=$4
			WHERE target.id=$1
			  AND target.network_id=$5 AND source.network_id=$5 AND token.network_id=$5
			  AND target.address=$9
			  AND target.deletion_mark=FALSE AND source.deletion_mark=FALSE AND token.deletion_mark=FALSE
			  AND token.token_standard='native'
			  AND settlement.network_id=$5 AND settlement.tx_hash=$6
			  AND settlement.native_token_id=$3
			  AND settlement.basis_type='CryptoNetworkOperation' AND settlement.basis_id=$8
			  AND settlement.finality_status IN ('solidified','finalized')
			  AND settlement.status=$7
		)
	`, doc.TargetWalletID, doc.SourceWalletID, doc.CostTokenID, *doc.ChainSettlementID,
		doc.NetworkID, doc.TxHash, doc.Status, doc.ID, doc.TargetAddress).Scan(&valid)
	if err != nil {
		return fmt.Errorf("validate crypto network operation posting references: %w", err)
	}
	if !valid {
		return apperror.NewBusinessRule("CRYPTO_NETWORK_OPERATION_REFERENCE_MISMATCH", "network operation wallets, native token, transaction and final settlement must match")
	}
	return nil
}

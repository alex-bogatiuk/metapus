package crypto_document_repo

import (
	"context"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_network_operation"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/document_repo"
)

type CryptoNetworkOperationRepo struct {
	*document_repo.BaseDocumentRepo[*crypto_network_operation.CryptoNetworkOperation]
}

func NewCryptoNetworkOperationRepo() *CryptoNetworkOperationRepo {
	return &CryptoNetworkOperationRepo{BaseDocumentRepo: document_repo.NewDocumentRepoForDefinition[*crypto_network_operation.CryptoNetworkOperation](crypto_network_operation.Definition,
		postgres.ExtractDBColumns[crypto_network_operation.CryptoNetworkOperation](),
		func() *crypto_network_operation.CryptoNetworkOperation {
			return &crypto_network_operation.CryptoNetworkOperation{}
		},
	)}
}

func (r *CryptoNetworkOperationRepo) ValidatePostingReferences(ctx context.Context, doc *crypto_network_operation.CryptoNetworkOperation) error {
	if doc.ChainSettlementID == nil {
		return apperror.NewBusinessRule("CRYPTO_NETWORK_OPERATION_SETTLEMENT_REQUIRED", "final chain settlement is required")
	}
	var valid bool
	err := r.GetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
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

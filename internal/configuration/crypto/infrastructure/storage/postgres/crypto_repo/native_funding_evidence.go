package crypto_repo

import (
	"context"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

type NativeFundingEvidenceRepo struct {
	*BlockIngestionRepo
}

func (NativeFundingEvidenceRepo) ExpectedFundingTransfer(ctx context.Context, fundingID id.ID, receipt crypto.ChainReceipt) (crypto.BlockchainEvent, error) {
	event := crypto.BlockchainEvent{EventKind: crypto.EventKindNativeValue, EventType: crypto.EventTypeTransfer,
		BlockNumber: receipt.BlockHeight, BlockHash: receipt.BlockHash, Confirmations: receipt.Confirmations,
		Finality: crypto.FinalitySolidified, Canonicality: crypto.CanonicalityCanonical}
	var family string
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT f.network_id,f.tx_hash,f.amount_minor,s.address,d.address,n.chain_family
		FROM doc_crypto_native_fundings f JOIN cat_wallets s ON s.id=f.source_wallet_id
		JOIN cat_wallets d ON d.id=f.destination_wallet_id JOIN cat_blockchain_networks n ON n.id=f.network_id
		WHERE f.id=$1 AND f.network_id=$2 AND f.tx_hash=$3`, fundingID, receipt.NetworkID, receipt.TxHash).
		Scan(&event.NetworkID, &event.TxHash, &event.Amount, &event.FromAddress, &event.ToAddress, &family)
	if err != nil {
		return event, err
	}
	event.DecoderVersion, err = crypto.TransferEvidenceSchemaFor(crypto.ChainFamily(family))
	return event, err
}

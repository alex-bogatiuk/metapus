package outbound_repo

import (
	"context"
	"fmt"

	"metapus/internal/application/outboundapp"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

var ErrMerchantCustody = outboundapp.ErrMerchantCustody

type CustodyWallet = outboundapp.CustodyWallet

func loadCustodyWallet(ctx context.Context, walletID id.ID) (CustodyWallet, error) {
	var wallet CustodyWallet
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT w.id,w.network_id,w.address,w.vault_key_id,w.wallet_role,w.custody_merchant_id,n.chain_family
		FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id
		WHERE w.id=$1 AND w.is_active AND NOT w.deletion_mark`, walletID).Scan(
		&wallet.ID, &wallet.NetworkID, &wallet.Address, &wallet.VaultKeyID, &wallet.Role, &wallet.MerchantID, &wallet.Family)
	if err != nil {
		return wallet, fmt.Errorf("%w: source wallet unavailable: %v", ErrMerchantCustody, err)
	}
	return wallet, nil
}

func findCustodyWallet(ctx context.Context, networkID id.ID, address string) (CustodyWallet, error) {
	var walletID id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `
		SELECT w.id FROM cat_wallets w JOIN cat_blockchain_networks n ON n.id=w.network_id
		WHERE w.network_id=$1 AND w.address_key=CASE WHEN n.chain_family='evm' THEN LOWER($2) ELSE $2 END`, networkID, address).Scan(&walletID)
	if err != nil {
		return CustodyWallet{}, fmt.Errorf("%w: destination wallet unavailable: %v", ErrMerchantCustody, err)
	}
	return loadCustodyWallet(ctx, walletID)
}

func ownerMatchesMerchant(owner *id.ID, merchant id.ID, enforced bool) bool {
	return crypto.CustodyOwnerMatchesMerchant(owner, merchant, enforced)
}

package crypto_repo

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/pkg/sdk/postgres"
)

type WalletFundingJournalRepo struct{}

func NewWalletFundingJournalRepo() *WalletFundingJournalRepo { return &WalletFundingJournalRepo{} }

var _ cryptoapp.WalletFundingJournalRepository = (*WalletFundingJournalRepo)(nil)

func (*WalletFundingJournalRepo) List(ctx context.Context, limit, offset int) (cryptoapp.WalletFundingList, error) {
	result := cryptoapp.WalletFundingList{Items: []cryptoapp.WalletFundingRow{}}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	if err := q.QueryRow(ctx, `SELECT count(*) FROM reg_crypto_wallet_fundings`).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count wallet fundings: %w", err)
	}
	err := pgxscan.Select(ctx, q, &result.Items, `
		SELECT f.id::text,n.name AS network_name,f.wallet_id::text,w.code AS wallet_code,f.token_id::text,
		       t.symbol AS token_symbol,t.decimal_places,f.tx_hash,f.from_address,f.to_address,f.amount::text,
		       f.block_number,f.confirmations,f.block_timestamp,f.recorded_at
		FROM reg_crypto_wallet_fundings f
		JOIN cat_blockchain_networks n ON n.id=f.network_id JOIN cat_wallets w ON w.id=f.wallet_id
		JOIN cat_tokens t ON t.id=f.token_id
		ORDER BY f.recorded_at DESC,f.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return result, fmt.Errorf("list wallet fundings: %w", err)
	}
	return result, nil
}

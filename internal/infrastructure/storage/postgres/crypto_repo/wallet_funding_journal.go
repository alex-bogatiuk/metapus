package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"metapus/internal/infrastructure/storage/postgres"
)

// WalletFundingRow is one recorded external funding of a platform wallet:
// it added custody directly, without passing through unidentified receipts.
type WalletFundingRow struct {
	ID             string    `db:"id"`
	NetworkName    string    `db:"network_name"`
	WalletID       string    `db:"wallet_id"`
	WalletCode     string    `db:"wallet_code"`
	TokenID        string    `db:"token_id"`
	TokenSymbol    string    `db:"token_symbol"`
	DecimalPlaces  int       `db:"decimal_places"`
	TxHash         string    `db:"tx_hash"`
	FromAddress    string    `db:"from_address"`
	ToAddress      string    `db:"to_address"`
	Amount         string    `db:"amount"`
	BlockNumber    int64     `db:"block_number"`
	Confirmations  int       `db:"confirmations"`
	BlockTimestamp time.Time `db:"block_timestamp"`
	RecordedAt     time.Time `db:"recorded_at"`
}

type WalletFundingList struct {
	Items []WalletFundingRow
	Total int
}

type WalletFundingJournalRepo struct{}

func NewWalletFundingJournalRepo() *WalletFundingJournalRepo { return &WalletFundingJournalRepo{} }

func (*WalletFundingJournalRepo) List(ctx context.Context, limit, offset int) (WalletFundingList, error) {
	result := WalletFundingList{Items: []WalletFundingRow{}}
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

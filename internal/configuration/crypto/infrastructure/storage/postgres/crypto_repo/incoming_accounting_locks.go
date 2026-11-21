package crypto_repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type custodyRecipientKey struct{ wallet, token id.ID }

func custodyRecipientKeys(ctx context.Context, event crypto.BlockchainEvent) ([]custodyRecipientKey, error) {
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT w.id,t.id FROM cat_wallets w
		JOIN cat_blockchain_networks n ON n.id=w.network_id JOIN cat_tokens t ON t.network_id=n.id
		WHERE n.id=$1 AND w.address_key=CASE WHEN n.chain_family='evm' THEN lower($2) ELSE $2 END
		AND CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,''))=lower($3) ELSE COALESCE(t.contract_address,'')=$3 END
		ORDER BY w.id,t.id`, event.NetworkID, event.ToAddress, event.TokenContract)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []custodyRecipientKey
	for rows.Next() {
		var key custodyRecipientKey
		if err := rows.Scan(&key.wallet, &key.token); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// Proof observation also locks catalog wallets before inserting balance rows:
// their wallet foreign key is a real lock even when the balance did not exist.
func lockVerificationRecipient(ctx context.Context, event crypto.BlockchainEvent) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("incoming evidence lock requires a transaction")
	}
	keys, err := custodyRecipientKeys(ctx, event)
	if err != nil {
		return err
	}
	wallets := make([]id.ID, 0, len(keys))
	for _, key := range keys {
		wallets = append(wallets, key.wallet)
	}
	return NewCustodyRecognitionRepo().LockWallets(ctx, wallets...)
}

// CaptureBlock owns the ingestion checkpoint while it processes every event.
// Take the union first: no later event may introduce a scope/wallet after an
// earlier event's checkpoint, balance or effect lock.
func lockCaptureRecipients(ctx context.Context, events []crypto.BlockchainEvent) error {
	if len(events) == 0 {
		return nil
	}
	networks := make([]id.ID, 0, len(events))
	addresses := make([]string, 0, len(events))
	contracts := make([]string, 0, len(events))
	for _, event := range events {
		networks = append(networks, event.NetworkID)
		addresses = append(addresses, event.ToAddress)
		contracts = append(contracts, event.TokenContract)
	}
	rows, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Query(ctx, `SELECT DISTINCT w.id
		FROM unnest($1::uuid[],$2::text[],$3::text[]) e(network_id,address,contract)
		JOIN cat_blockchain_networks n ON n.id=e.network_id
		JOIN cat_wallets w ON w.network_id=n.id AND w.address_key=CASE WHEN n.chain_family='evm' THEN lower(e.address) ELSE e.address END
		JOIN cat_tokens t ON t.network_id=n.id AND CASE WHEN n.chain_family='evm' THEN lower(COALESCE(t.contract_address,''))=lower(e.contract) ELSE COALESCE(t.contract_address,'')=e.contract END
		ORDER BY w.id`, networks, addresses, contracts)
	if err != nil {
		return err
	}
	var wallets []id.ID
	for rows.Next() {
		var wallet id.ID
		if err := rows.Scan(&wallet); err != nil {
			rows.Close()
			return err
		}
		wallets = append(wallets, wallet)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return NewCustodyRecognitionRepo().LockWallets(ctx, wallets...)
}

func (*BlockIngestionRepo) LockIncomingEvent(ctx context.Context, event crypto.BlockchainEvent) error {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var payment, wallet, asset id.ID
	err := q.QueryRow(ctx, `SELECT id,wallet_id,token_id FROM doc_crypto_payments
		WHERE network_id=$1 AND tx_hash=$2 AND event_kind=$3 AND event_index=$4`, event.NetworkID, event.TxHash, event.EffectiveEventKind(), event.EventIndex).
		Scan(&payment, &wallet, &asset)
	if err == nil {
		if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoPayment", payment); err != nil {
			return err
		}
		return NewCustodyRecognitionRepo().LockWallet(ctx, wallet, asset)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return lockVerificationRecipient(ctx, event)
}

var _ crypto.IncomingFinancialLocker = (*BlockIngestionRepo)(nil)

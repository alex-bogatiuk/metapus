package crypto_repo

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/catalogs/token"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// sweepCandidate holds wallet info with its accumulated Balance metadata.
type WorkerSweepCandidate struct {
	WalletID        id.ID
	MerchantID      *id.ID
	TokenID         id.ID
	Balance         types.RawAmount
	LastSweptAt     *time.Time
	OldestPaymentAt *time.Time // MIN(confirmed_at) — for max-age check on never-swept wallets
}

type WorkerNetwork struct {
	Ref    crypto.NetworkRef
	Assets []crypto.AssetRef
}

// querySweepCandidates loads pool wallets with accumulated confirmed payments since last sweep.
// Only wallets NOT already in sweep_pending or frozen.
// Groups by wallet to sum all confirmed payments since last_swept_at.
func (r *WorkerRuntimeRepo) QuerySweepCandidates(ctx context.Context, limit int) ([]WorkerSweepCandidate, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	const query = `
		SELECT w.id AS wallet_id,
		       COALESCE(w.custody_merchant_id,w.merchant_id),
		       w.last_swept_at,
		       p.token_id,
		       COALESCE(SUM(p.amount), 0) AS balance,
		       MIN(p.confirmed_at)        AS oldest_payment_at
		FROM cat_wallets w
		INNER JOIN doc_crypto_payments p ON p.wallet_id = w.id
		WHERE w.tier = 'pool'
		  AND w.wallet_role = 'merchant_pool'
		  AND w.status IN ('free', 'assigned')
		  AND w.deposit_retired_at IS NULL
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
		  AND p.status IN ('confirmed', 'settled')
		  AND p.confirmed_at > COALESCE(w.last_swept_at, '1970-01-01'::timestamptz)
		GROUP BY w.id, w.custody_merchant_id, w.merchant_id, w.last_swept_at, p.token_id
		HAVING COALESCE(SUM(p.amount), 0) > 0
		LIMIT $1
	`

	rows, err := querier.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query sweep candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]WorkerSweepCandidate, 0, limit)
	for rows.Next() {
		var c WorkerSweepCandidate
		if err := rows.Scan(&c.WalletID, &c.MerchantID, &c.LastSweptAt, &c.TokenID, &c.Balance, &c.OldestPaymentAt); err != nil {
			return nil, fmt.Errorf("scan sweep candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sweep candidates: %w", err)
	}

	return candidates, nil
}

// loadNetworksFiltered reads the complete runtime catalog in one SQL snapshot.
// A row limit on the network/token join can silently omit networks or split an
// asset set (including its native token). This is discovery, not a paged list.
func (r *WorkerRuntimeRepo) LoadNetworksFiltered(ctx context.Context, activeOnly bool, networkCode string) ([]WorkerNetwork, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		SELECT n.id, n.code, n.chain_family, n.network_identifier, n.native_token_symbol,
		       n.native_decimals, n.confirmations_needed, n.finality_mode,
		       t.id, t.code, t.name, t.symbol, t.token_standard, COALESCE(t.contract_address, ''), t.decimal_places
		FROM cat_blockchain_networks n
		JOIN cat_tokens t ON t.network_id = n.id
		 AND t.is_active = TRUE
		 AND t.deletion_mark = FALSE
		WHERE ($1=FALSE OR n.is_active=TRUE)
		  AND ($2='' OR n.code=$2)
		ORDER BY n.id, t.code
	`, activeOnly, networkCode)
	if err != nil {
		return nil, fmt.Errorf("query networks: %w", err)
	}
	defer rows.Close()

	var networks []WorkerNetwork
	indexByID := make(map[id.ID]int)
	for rows.Next() {
		var networkID, tokenID id.ID
		var code, networkIdentifier, nativeTokenSymbol, contract, tokenCode, tokenName, tokenSymbol string
		var chainFamily crypto.ChainFamily
		var standard token.TokenStandard
		var finalityMode crypto.FinalityMode
		var nativeDecimals, tokenDecimals, requiredConfs int
		if err := rows.Scan(&networkID, &code, &chainFamily, &networkIdentifier, &nativeTokenSymbol, &nativeDecimals, &requiredConfs, &finalityMode, &tokenID, &tokenCode, &tokenName, &tokenSymbol, &standard, &contract, &tokenDecimals); err != nil {
			return nil, fmt.Errorf("scan network: %w", err)
		}
		idx, exists := indexByID[networkID]
		if !exists {
			idx = len(networks)
			indexByID[networkID] = idx
			networks = append(networks, WorkerNetwork{
				Ref: crypto.NetworkRef{
					ID: networkID, Code: code, Family: chainFamily,
					NetworkIdentifier: networkIdentifier, NativeTokenSymbol: nativeTokenSymbol,
					NativeDecimals: nativeDecimals, RequiredConfirmations: requiredConfs,
					FinalityMode: finalityMode,
				},
			})
		}
		asset := crypto.AssetRef{TokenID: tokenID, Code: tokenCode, Name: tokenName, Symbol: tokenSymbol, Standard: standard, ContractAddress: contract, Decimals: tokenDecimals}
		networks[idx].Assets = append(networks[idx].Assets, asset)
		if standard == token.TokenStandardNative {
			if !id.IsNil(networks[idx].Ref.NativeTokenID) {
				return nil, fmt.Errorf("network %s has more than one active native token", code)
			}
			networks[idx].Ref.NativeTokenID = tokenID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, network := range networks {
		if err := network.Ref.Validate(); err != nil {
			return nil, fmt.Errorf("invalid active network %s: %w", network.Ref.Code, err)
		}
	}
	return networks, nil
}

// loadWalletAddresses loads all active Vault-managed wallet addresses for a network.
func (r *WorkerRuntimeRepo) LoadWalletAddresses(ctx context.Context, networkID id.ID) ([]string, error) {
	txm := postgres.MustGetTxManager(ctx)
	querier := txm.GetQuerier(ctx)

	rows, err := querier.Query(ctx, `
		SELECT address FROM cat_wallets
		WHERE network_id = $1 
          -- An immutable merchant address stays observable after retirement
          -- or catalog deactivation. Re-enabling it must not hide an intake gap.
          AND (custody_merchant_id IS NOT NULL OR (is_active = TRUE AND deletion_mark = FALSE))
		  AND vault_key_id <> ''
		ORDER BY code
	`, networkID)
	if err != nil {
		return nil, fmt.Errorf("query wallet addresses: %w", err)
	}
	defer rows.Close()

	var addresses []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, fmt.Errorf("scan address: %w", err)
		}
		addresses = append(addresses, addr)
	}

	return addresses, rows.Err()
}

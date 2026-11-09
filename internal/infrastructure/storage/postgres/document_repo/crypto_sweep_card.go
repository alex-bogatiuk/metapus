package document_repo

import (
	"context"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_sweep"
)

// CryptoSweepCardData is a read projection. Amounts remain exact minor-unit strings.
type CryptoSweepCardData struct {
	Tokens        []CryptoSweepCardToken
	Wallets       []CryptoSweepCardWallet
	NetworkFees   []CryptoSweepCardNetworkFee
	ResourceCosts []CryptoSweepCardResourceCost
	Attempts      []CryptoSweepCardAttempt
}

type CryptoSweepCardAttempt struct {
	LineID       id.ID  `db:"line_id"`
	Status       string `db:"status"`
	ErrorMessage string `db:"error_message"`
	Retryable    bool   `db:"retryable"`
}

type CryptoSweepCardToken struct {
	ID            id.ID  `db:"id"`
	Symbol        string `db:"symbol"`
	DecimalPlaces int    `db:"decimal_places"`
	NetworkName   string `db:"network_name"`
	ExplorerURL   string `db:"explorer_url"`
}

type CryptoSweepCardWallet struct {
	ID      id.ID  `db:"id"`
	Name    string `db:"name"`
	Address string `db:"address"`
}

type CryptoSweepCardNetworkFee struct {
	LineID       id.ID  `db:"line_id"`
	SettlementID id.ID  `db:"settlement_id"`
	CostTokenID  id.ID  `db:"cost_token_id"`
	Amount       string `db:"amount"`
	TxHash       string `db:"tx_hash"`
}

type CryptoSweepCardResourceCost struct {
	LineID       id.ID  `db:"line_id"`
	AllocationID id.ID  `db:"allocation_id"`
	RentalID     id.ID  `db:"rental_id"`
	RentalNumber string `db:"rental_number"`
	Status       string `db:"status"`
	CostTokenID  id.ID  `db:"cost_token_id"`
	Amount       string `db:"amount"`
}

// ReadCardData loads the accounting facts for visible sweep lines in batches.
// The caller applies document and register FLS before exposing this projection.
func (r *CryptoSweepRepo) ReadCardData(ctx context.Context, doc *crypto_sweep.CryptoSweep) (CryptoSweepCardData, error) {
	data := CryptoSweepCardData{
		Tokens: []CryptoSweepCardToken{}, Wallets: []CryptoSweepCardWallet{},
		NetworkFees: []CryptoSweepCardNetworkFee{}, ResourceCosts: []CryptoSweepCardResourceCost{},
		Attempts: []CryptoSweepCardAttempt{},
	}
	lineIDs := make([]id.ID, 0, len(doc.Lines))
	walletIDs := make([]id.ID, 0, len(doc.Lines)+1)
	tokenIDs := make([]id.ID, 0, len(doc.Lines)+1)
	if !id.IsNil(doc.TokenID) {
		tokenIDs = append(tokenIDs, doc.TokenID)
	}
	if !id.IsNil(doc.HotWalletID) {
		walletIDs = append(walletIDs, doc.HotWalletID)
	}
	for _, line := range doc.Lines {
		if !id.IsNil(line.LineID) {
			lineIDs = append(lineIDs, line.LineID)
		}
		if !id.IsNil(line.WalletID) {
			walletIDs = append(walletIDs, line.WalletID)
		}
		if line.NetworkFeeTokenID != nil && !id.IsNil(*line.NetworkFeeTokenID) {
			tokenIDs = append(tokenIDs, *line.NetworkFeeTokenID)
		}
	}
	querier := r.getTxManager(ctx).GetQuerier(ctx)
	if len(lineIDs) > 0 {
		if err := pgxscan.Select(ctx, querier, &data.Attempts, `
			SELECT a.id AS line_id,
			       a.status,
			       a.error_message,
			       (a.status = 'held' AND a.broadcast_at IS NULL AND NOT EXISTS (
			           SELECT 1 FROM doc_crypto_sweep_lines l
			           WHERE l.line_id = a.id AND l.tx_hash <> ''
			       ) AND NOT EXISTS (
			           SELECT 1 FROM reg_crypto_signing_authorizations s
			           WHERE s.basis_type = 'CryptoSweepAttempt' AND s.basis_id = a.id
			       ) AND NOT EXISTS (
			           SELECT 1 FROM reg_crypto_fee_reservations f
			           WHERE f.basis_type = 'CryptoSweepAttempt' AND f.basis_id = a.id AND f.tx_hash <> ''
			       ) AND NOT EXISTS (
			           SELECT 1 FROM doc_crypto_resource_rentals r
			           WHERE r.basis_type = 'CryptoSweep' AND r.basis_id = $1
			             AND r.receive_wallet_id = a.wallet_id AND r.deletion_mark = FALSE
			             AND r.status = 'held' AND r.external_serial <> ''
			       )) AS retryable
			FROM reg_crypto_sweep_attempts a
			WHERE a.sweep_id = $1 AND a.id = ANY($2::uuid[])`, doc.ID, lineIDs); err != nil {
			return data, fmt.Errorf("read sweep attempts: %w", err)
		}
		if err := pgxscan.Select(ctx, querier, &data.NetworkFees, `
			SELECT l.line_id, s.id AS settlement_id, c.cost_token_id, c.amount::text AS amount, s.tx_hash
			FROM doc_crypto_sweep_lines l
			JOIN reg_crypto_chain_settlements s ON s.basis_type='CryptoSweepAttempt' AND s.basis_id=l.line_id
			JOIN reg_crypto_platform_cost_movements c ON c.recorder_type='ChainSettlement'
				AND c.recorder_id=l.line_id AND c.cost_type='chain_network_fee'
				AND c.purpose='sweep' AND c.record_type='expense'
			WHERE l.document_id=$1 AND l.line_id=ANY($2::uuid[])
			ORDER BY l.line_no`, doc.ID, lineIDs); err != nil {
			return data, fmt.Errorf("read sweep network costs: %w", err)
		}
		if err := pgxscan.Select(ctx, querier, &data.ResourceCosts, `
			SELECT l.line_id, a.id AS allocation_id, a.rental_id, r.number AS rental_number,
				a.status, r.cost_token_id, a.allocated_cost_amount::text AS amount
			FROM doc_crypto_sweep_lines l
			JOIN reg_crypto_resource_allocations a ON a.basis_type='CryptoSweepLine'
				AND a.basis_id=l.line_id AND a.purpose='sweep'
			JOIN doc_crypto_resource_rentals r ON r.id=a.rental_id
			WHERE l.document_id=$1 AND l.line_id=ANY($2::uuid[])
			ORDER BY l.line_no, a.generation`, doc.ID, lineIDs); err != nil {
			return data, fmt.Errorf("read sweep resource allocations: %w", err)
		}
	}
	for _, fee := range data.NetworkFees {
		tokenIDs = append(tokenIDs, fee.CostTokenID)
	}
	for _, cost := range data.ResourceCosts {
		tokenIDs = append(tokenIDs, cost.CostTokenID)
	}
	if len(tokenIDs) > 0 {
		if err := pgxscan.Select(ctx, querier, &data.Tokens,
			`SELECT t.id, t.symbol, t.decimal_places, n.name AS network_name, n.explorer_url
			 FROM cat_tokens t JOIN cat_blockchain_networks n ON n.id=t.network_id
			 WHERE t.id=ANY($1::uuid[])`, tokenIDs); err != nil {
			return data, fmt.Errorf("read sweep tokens: %w", err)
		}
	}
	if len(walletIDs) > 0 {
		if err := pgxscan.Select(ctx, querier, &data.Wallets,
			`SELECT id, name, address FROM cat_wallets WHERE id=ANY($1::uuid[])`, walletIDs); err != nil {
			return data, fmt.Errorf("read sweep wallets: %w", err)
		}
	}
	return data, nil
}

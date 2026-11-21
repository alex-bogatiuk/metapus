// Package funding persists chain-verified external funding of platform wallets.
// Merchant-owned operating capital uses the AML operating receipt workflow.
package funding

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/catalogs/wallet"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain"
	"metapus/internal/infrastructure/storage/postgres"
)

type Target struct {
	WalletID, NetworkID, TokenID id.ID
	TokenCode                    string
	TokenStandard                token.TokenStandard
	ContractAddress              string
	Address                      string
}

type ConfirmedTransfer struct {
	VerificationID *id.ID
	EventKind      crypto.EventKind
	EventIndex     int64
	BlockHash      string
	FromAddress    string
	ToAddress      string
	Amount         types.RawAmount
	BlockNumber    int64
	BlockTimestamp time.Time
	Confirmations  int
}

type Result struct {
	ID            id.ID           `json:"id"`
	WalletCode    string          `json:"walletCode"`
	TokenCode     string          `json:"tokenCode"`
	TxHash        string          `json:"txHash"`
	Amount        types.RawAmount `json:"amount"`
	BlockNumber   int64           `json:"blockNumber"`
	Confirmations int             `json:"confirmations"`
	Existing      bool            `json:"existing"`
}

type fundingRecordedPayload struct {
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	Action     string `json:"action"`
	Funding    Result `json:"funding"`
}

// Record atomically writes the funding receipt, shared principal recognition,
// physical movement, audit and outbox. Indexed independent proof is mandatory
// even on a network without ingestion cutover. Replay uses occurrence identity.
func Record(ctx context.Context, walletCode string, target Target, txHash string, transfer ConfirmedTransfer) (Result, error) {
	return record(ctx, walletCode, target, txHash, transfer, postgres.NewOutboxPublisher())
}

func record(ctx context.Context, walletCode string, target Target, txHash string, transfer ConfirmedTransfer, publisher domain.OutboxPublisher) (Result, error) {
	result := Result{WalletCode: walletCode, TokenCode: target.TokenCode, TxHash: txHash,
		Amount: transfer.Amount, BlockNumber: transfer.BlockNumber, Confirmations: transfer.Confirmations}
	txm := postgres.MustGetTxManager(ctx)
	err := txm.RunInTransaction(ctx, func(ctx context.Context) error {
		q := txm.GetQuerier(ctx)
		var required int
		var owner *id.ID
		var standard string
		if err := q.QueryRow(ctx, `SELECT n.confirmations_needed,w.custody_merchant_id,t.token_standard
			FROM cat_blockchain_networks n JOIN cat_wallets w ON w.network_id=n.id
			JOIN cat_tokens t ON t.network_id=n.id WHERE n.id=$1 AND w.id=$2 AND t.id=$3`,
			target.NetworkID, target.WalletID, target.TokenID).Scan(&required, &owner, &standard); err != nil {
			return err
		}
		if owner != nil {
			return apperror.NewConflict("merchant-owned funding requires its operating receipt workflow")
		}
		basis := crypto.AccountingTransferBasis{NetworkID: target.NetworkID, WalletID: target.WalletID, TokenID: target.TokenID,
			TxHash: txHash, EventKind: transfer.EventKind, EventIndex: transfer.EventIndex, FromAddress: transfer.FromAddress,
			Amount: transfer.Amount, BlockNumber: transfer.BlockNumber, BlockHash: transfer.BlockHash,
			RequiredConfs: required, VerificationID: transfer.VerificationID}
		registry := crypto_repo.NewCustodyRecognitionRepo()
		if err := registry.LockIncoming(ctx, basis); err != nil {
			return err
		}
		guard := crypto_repo.NewBlockIngestionRepo()
		event, err := guard.ExpectedAccountingTransfer(ctx, basis)
		if err != nil {
			return err
		}
		if event.ToAddress != transfer.ToAddress || !event.Timestamp.Equal(transfer.BlockTimestamp) {
			return crypto.ErrVerificationMismatch
		}
		event.VerificationID, event.Confirmations = transfer.VerificationID, transfer.Confirmations
		event.Finality, event.Canonicality = crypto.FinalitySolidified, crypto.CanonicalityCanonical
		evidence, err := guard.AuthorizeCustodyEffect(ctx, basis, event)
		if err != nil {
			return fmt.Errorf("wallet funding transfer evidence: %w", err)
		}
		var storedWallet, storedToken id.ID
		var storedAmount types.RawAmount
		err = q.QueryRow(ctx, `SELECT id,wallet_id,token_id,amount FROM reg_crypto_wallet_fundings WHERE occurrence_id=$1`,
			evidence.OccurrenceID).Scan(&result.ID, &storedWallet, &storedToken, &storedAmount)
		if err == nil {
			if storedWallet != target.WalletID || storedToken != target.TokenID || !storedAmount.Equal(transfer.Amount) {
				return apperror.NewConflict("wallet funding replay differs from recorded receipt")
			}
			result.Existing = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		result.ID = id.New()
		postingID := id.New()
		if err := registry.ClaimIncoming(ctx, evidence, postingID, result.ID, "CryptoWalletFunding",
			target.WalletID, target.TokenID, result.ID); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_wallet_fundings(
			id,network_id,wallet_id,token_id,tx_hash,from_address,to_address,amount,block_number,
			confirmations,block_timestamp,verification_id,occurrence_id,posting_id)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			result.ID, target.NetworkID, target.WalletID, target.TokenID, txHash, transfer.FromAddress,
			transfer.ToAddress, transfer.Amount, transfer.BlockNumber, transfer.Confirmations,
			transfer.BlockTimestamp, evidence.VerificationID, evidence.OccurrenceID, postingID); err != nil {
			return fmt.Errorf("insert recognized wallet funding: %w", err)
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_balance_movements(
			line_id,recorder_id,recorder_type,recorder_version,period,record_type,wallet_id,token_id,amount)
			VALUES($1,$1,'CryptoWalletFunding',1,$2,'receipt',$3,$4,$5)`,
			result.ID, transfer.BlockTimestamp, target.WalletID, target.TokenID, transfer.Amount); err != nil {
			return fmt.Errorf("record wallet funding physical movement: %w", err)
		}
		if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_wallet_funding_audit(funding_id,verification_id,posting_id,actor)
			VALUES($1,$2,$3,'wallet-funding-cli')`, result.ID, evidence.VerificationID, postingID); err != nil {
			return err
		}
		if standard == string(token.TokenStandardNative) {
			if _, err := q.Exec(ctx, `UPDATE cat_wallets SET chain_activation_status=$2,
				version=version+1,updated_at=NOW() WHERE id=$1 AND is_active AND NOT deletion_mark
				AND chain_activation_status<>$2`, target.WalletID, wallet.ChainActivationActive); err != nil {
				return fmt.Errorf("mark natively funded wallet active: %w", err)
			}
		}
		return publisher.Publish(ctx, domain.DomainEvent{
			AggregateType: "CryptoWalletFunding", AggregateID: result.ID, EventType: "recorded",
			Payload: fundingRecordedPayload{EntityType: "CryptoWalletFunding", EntityID: result.ID.String(), Action: "recorded", Funding: result},
		})
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

package funding

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/domain/catalogs/token"
	"metapus/internal/configuration/crypto/domain/crypto"
	chainfunding "metapus/internal/configuration/crypto/infrastructure/blockchain/funding"
	"metapus/internal/configuration/crypto/infrastructure/blockchain/tron"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/postgres"
	"strings"
)

type nativeFundingChain interface {
	VerifyIncomingTransfer(context.Context, id.ID, string, string, string) (crypto.TransferVerification, error)
}
type WalletFundingResult = chainfunding.Result
type walletFundingTarget struct {
	walletID              id.ID
	networkID             id.ID
	tokenID               id.ID
	tokenCode             string
	tokenStandard         string
	contractAddress       string
	address               string
	requiredConfirmations int
}

// RecordConfirmedWalletFunding uses the same independent block evidence as payments.
func RecordConfirmedWalletFunding(ctx context.Context, chain nativeFundingChain, walletCode, tokenCode, txHash string) (WalletFundingResult, error) {
	walletCode = strings.TrimSpace(walletCode)
	if walletCode == "" {
		return WalletFundingResult{}, fmt.Errorf("wallet code is required")
	}
	tokenCode = strings.TrimSpace(tokenCode)
	if tokenCode == "" {
		return WalletFundingResult{}, fmt.Errorf("token code is required")
	}
	txHash, err := normalizeFundingTxHash(txHash)
	if err != nil {
		return WalletFundingResult{}, err
	}

	txm := postgres.MustGetTxManager(ctx)
	q := txm.GetQuerier(ctx)
	var target walletFundingTarget
	err = q.QueryRow(ctx, `
		SELECT w.id, w.network_id, t.id, t.code, t.token_standard,
		       t.contract_address,
		       w.address, n.confirmations_needed
		FROM cat_wallets w
		JOIN cat_blockchain_networks n
		  ON n.id = w.network_id
		 AND n.is_active = TRUE
		 AND n.deletion_mark = FALSE
		JOIN cat_tokens t
		  ON t.network_id = w.network_id
		 AND t.code = $2
		 AND t.token_standard IN ($3, $4)
		 AND t.is_active = TRUE
		 AND t.deletion_mark = FALSE
		WHERE w.code = $1
		  AND w.is_active = TRUE
		  AND w.deletion_mark = FALSE
	`, walletCode, tokenCode, token.TokenStandardNative, token.TokenStandardTRC20).Scan(
		&target.walletID,
		&target.networkID,
		&target.tokenID,
		&target.tokenCode,
		&target.tokenStandard,
		&target.contractAddress,
		&target.address,
		&target.requiredConfirmations,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return WalletFundingResult{}, fmt.Errorf(
			"active managed wallet %q with active token %q was not found",
			walletCode,
			tokenCode,
		)
	}
	if err != nil {
		return WalletFundingResult{}, fmt.Errorf("load wallet funding target: %w", err)
	}

	var active bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sys_crypto_block_ingestion WHERE network_id=$1 AND activated_at IS NOT NULL AND mode IN ('solidified','hybrid'))`, target.networkID).Scan(&active); err != nil {
		return WalletFundingResult{}, err
	}
	if !active {
		return WalletFundingResult{}, crypto.ErrVerificationPending
	}
	proof, err := chain.VerifyIncomingTransfer(ctx, target.networkID, txHash, target.address, target.contractAddress)
	if err != nil {
		return WalletFundingResult{}, err
	}
	if err := validateFundingProof(target, txHash, proof); err != nil {
		return WalletFundingResult{}, err
	}
	proofID, err := crypto_repo.NewBlockIngestionRepo().SaveVerification(ctx, proof)
	if err != nil {
		return WalletFundingResult{}, err
	}
	return chainfunding.Record(ctx, walletCode, chainfunding.Target{WalletID: target.walletID, NetworkID: target.networkID, TokenID: target.tokenID, TokenCode: target.tokenCode, TokenStandard: token.TokenStandard(target.tokenStandard), ContractAddress: target.contractAddress, Address: target.address}, txHash, chainfunding.ConfirmedTransfer{FromAddress: proof.Event.FromAddress, ToAddress: proof.Event.ToAddress, Amount: proof.Event.Amount, BlockNumber: proof.Event.BlockNumber, BlockHash: proof.Event.BlockHash, BlockTimestamp: proof.Event.Timestamp, Confirmations: proof.Event.Confirmations, VerificationID: &proofID, EventKind: proof.Event.EventKind, EventIndex: proof.Event.EventIndex})
}
func validateFundingProof(target walletFundingTarget, txHash string, proof crypto.TransferVerification) error {
	if err := proof.Validate(); err != nil {
		return err
	}
	if proof.AccountingIdentity == nil {
		return crypto.ErrVerificationPending
	}
	e := proof.Event
	if e.NetworkID != target.networkID || e.TxHash != txHash || e.ToAddress != tron.NormalizeTronAddress(target.address) || e.TokenContract != tron.NormalizeTronAddress(target.contractAddress) || proof.Outcome != "verified" || e.Canonicality != crypto.CanonicalityCanonical {
		return crypto.ErrVerificationMismatch
	}
	if e.Confirmations < target.requiredConfirmations {
		return crypto.ErrVerificationPending
	}
	return nil
}
func normalizeFundingTxHash(txHash string) (string, error) {
	txHash = strings.ToLower(strings.TrimSpace(txHash))
	if len(txHash) != 64 {
		return "", fmt.Errorf("TRON transaction hash must contain 64 hexadecimal characters")
	}
	for _, r := range txHash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", fmt.Errorf("TRON transaction hash must contain 64 hexadecimal characters")
		}
	}
	return txHash, nil
}

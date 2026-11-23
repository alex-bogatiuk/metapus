package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/jackc/pgx/v5"
)

func (r *BlockIngestionRepo) AuthorizeCustodyEffect(
	ctx context.Context, basis crypto.AccountingTransferBasis, event crypto.BlockchainEvent,
) (crypto.VerifiedCustodyEffect, error) {
	return r.authorizeCustodyEffect(ctx, basis, event, false)
}

// Final settlement observes an accepted transfer even while new execution is
// paused. Evidence, finality and ownership constraints remain mandatory.
func (r *BlockIngestionRepo) AuthorizeSettlementCustodyEffect(ctx context.Context, basis crypto.AccountingTransferBasis, event crypto.BlockchainEvent) (crypto.VerifiedCustodyEffect, error) {
	return r.authorizeCustodyEffect(ctx, basis, event, true)
}

func (r *BlockIngestionRepo) authorizeCustodyEffect(ctx context.Context, basis crypto.AccountingTransferBasis, event crypto.BlockchainEvent, recovery bool) (crypto.VerifiedCustodyEffect, error) {
	var result crypto.VerifiedCustodyEffect
	if err := basis.Validate(); err != nil {
		return result, err
	}
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return result, apperror.NewValidation("custody effect verification requires a transaction")
	}
	if event.VerificationID == nil || *event.VerificationID != *basis.VerificationID ||
		event.NetworkID != basis.NetworkID || event.TxHash != basis.TxHash ||
		event.EventKind != basis.EventKind || event.EventIndex != basis.EventIndex ||
		event.BlockNumber != basis.BlockNumber || event.BlockHash != basis.BlockHash ||
		event.FromAddress == "" || event.ToAddress == "" || event.Amount.Cmp(basis.Amount) != 0 {
		return result, crypto.ErrVerificationMismatch
	}
	q := tm.GetQuerier(ctx)
	var required int
	err := q.QueryRow(ctx, `SELECT n.confirmations_needed
		FROM cat_blockchain_networks n JOIN cat_wallets w ON w.network_id=n.id
		JOIN cat_tokens t ON t.network_id=n.id
		WHERE n.id=$1 AND w.id=$2 AND t.id=$3 AND w.vault_key_id<>''
		FOR SHARE OF n,w,t`, basis.NetworkID, basis.WalletID, basis.TokenID).Scan(&required)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, crypto.ErrVerificationMismatch
	}
	if err != nil {
		return result, fmt.Errorf("load custody effect catalog basis: %w", err)
	}
	basis.RequiredConfs = max(required, basis.RequiredConfs)
	proofID, err := r.authorizeOutcomeWithRecovery(ctx, basis, event, crypto.CanonicalityCanonical, true, recovery)
	if err != nil {
		return result, err
	}
	if proofID != *basis.VerificationID {
		return result, crypto.ErrVerificationMismatch
	}
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT proof.evidence,indexed.occurrence_id
		FROM reg_crypto_transfer_verifications proof
		JOIN reg_crypto_accounting_effect_evidence indexed ON indexed.verification_id=proof.id
		WHERE proof.id=$1`, proofID).Scan(&raw, &result.OccurrenceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, crypto.ErrVerificationPending
		}
		return result, fmt.Errorf("load indexed custody effect: %w", err)
	}
	var proof crypto.TransferVerification
	if err := json.Unmarshal(raw, &proof); err != nil {
		return result, fmt.Errorf("decode custody effect proof: %w", err)
	}
	if err := proof.Validate(); err != nil {
		return result, err
	}
	if proof.AccountingIdentity == nil {
		return result, crypto.ErrVerificationPending
	}
	result.Occurrence, err = proof.AccountingIdentity.Occurrence(proof.Event)
	if err != nil {
		return crypto.VerifiedCustodyEffect{}, err
	}
	result.VerificationID = proofID
	return result, nil
}

var _ crypto.CustodyEffectGuard = (*BlockIngestionRepo)(nil)

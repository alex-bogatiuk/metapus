package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres"
)

// CustodyRecognitionRepo owns shared first-generation controlled principal
// claim. Workflow-specific admission and atomic financial sets remain with the
// caller; this repository does not turn historical evidence into permission.
type CustodyRecognitionRepo struct{}

func NewCustodyRecognitionRepo() *CustodyRecognitionRepo { return &CustodyRecognitionRepo{} }

func (r *CustodyRecognitionRepo) LockWallet(ctx context.Context, walletID, _ id.ID) error {
	return r.LockWallets(ctx, walletID)
}

// LockWallets acquires the complete scope set before catalog wallets. Callers
// touching multiple wallets must supply the whole set in one call.
func (*CustodyRecognitionRepo) LockWallets(ctx context.Context, wallets ...id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("custody wallet lock requires a transaction")
	}
	_, err := tm.GetQuerier(ctx).Exec(ctx, "SELECT fn_crypto_lock_custody_wallets($1::uuid[])", wallets)
	return err
}

// LockScopes precedes network policy locks when receipt admission and fee
// reservation both need the same wallet. It never locks balances or effects.
func (*CustodyRecognitionRepo) LockScopes(ctx context.Context, wallets ...id.ID) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("custody scope lock requires a transaction")
	}
	_, err := tm.GetQuerier(ctx).Exec(ctx, "SELECT fn_crypto_lock_custody_scopes($1::uuid[])", wallets)
	return err
}

// LockIncoming uses the same balance row as the SQL guard of legacy writers,
// then locks the logical effect independently of recorder or block aliases.
func (*CustodyRecognitionRepo) LockIncoming(ctx context.Context, basis crypto.AccountingTransferBasis) error {
	if basis.RecordType == entity.RecordTypeExpense {
		return apperror.NewValidation("incoming custody requires a receipt basis")
	}
	return NewCustodyRecognitionRepo().LockTransfer(ctx, basis)
}

func (*CustodyRecognitionRepo) LockTransfer(ctx context.Context, basis crypto.AccountingTransferBasis) error {
	if err := basis.Validate(); err != nil {
		return err
	}
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("custody recognition requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	if err := NewCustodyRecognitionRepo().LockWallet(ctx, basis.WalletID, basis.TokenID); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, "SELECT fn_crypto_lock_receipt_balance($1,$2)", basis.WalletID, basis.TokenID); err != nil {
		return fmt.Errorf("lock custody balance: %w", err)
	}
	var effectID id.ID
	err := q.QueryRow(ctx, `SELECT e.id FROM reg_crypto_accounting_effect_evidence v
		JOIN reg_crypto_accounting_effect_occurrences o ON o.id=v.occurrence_id
		JOIN reg_crypto_accounting_effects e ON e.id=o.effect_id
		WHERE v.verification_id=$1 AND e.network_id=$2 FOR UPDATE OF e`,
		basis.VerificationID, basis.NetworkID).Scan(&effectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return crypto.ErrVerificationPending
	}
	if err != nil {
		return fmt.Errorf("lock custody logical identity: %w", err)
	}
	return nil
}

func (*CustodyRecognitionRepo) ClaimIncoming(
	ctx context.Context, evidence crypto.VerifiedCustodyEffect, postingID, recorderID id.ID,
	recorderType string, walletID, tokenID, lineID id.ID,
) error {
	return NewCustodyRecognitionRepo().claimSide(ctx, evidence, postingID, recorderID, recorderType, walletID, tokenID, lineID, entity.RecordTypeReceipt)
}

func (*CustodyRecognitionRepo) ClaimOutgoing(ctx context.Context, evidence crypto.VerifiedCustodyEffect, postingID, recorderID id.ID,
	recorderType string, walletID, tokenID, lineID id.ID) error {
	return NewCustodyRecognitionRepo().claimSide(ctx, evidence, postingID, recorderID, recorderType, walletID, tokenID, lineID, entity.RecordTypeExpense)
}

func (*CustodyRecognitionRepo) claimSide(ctx context.Context, evidence crypto.VerifiedCustodyEffect, postingID, recorderID id.ID,
	recorderType string, walletID, tokenID, lineID id.ID, side entity.RecordType) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("custody recognition requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_custody_effect_postings(
		id,effect_id,occurrence_id,verification_id,recorder_id,recorder_type,recorder_version)
		SELECT $1,effect_id,id,$3,$4,$5,1 FROM reg_crypto_accounting_effect_occurrences WHERE id=$2`,
		postingID, evidence.OccurrenceID, evidence.VerificationID, recorderID, recorderType); err != nil {
		return fmt.Errorf("claim custody posting ownership: %w", err)
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_custody_effect_recognitions(
		posting_id,wallet_id,token_id,record_type,movement_line_id)
		VALUES($1,$2,$3,$5,$4)`, postingID, walletID, tokenID, lineID, side); err != nil {
		return fmt.Errorf("claim custody physical side: %w", err)
	}
	return nil
}

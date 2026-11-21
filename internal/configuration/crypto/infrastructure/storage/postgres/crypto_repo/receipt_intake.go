package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"metapus/internal/configuration/crypto/application/cryptoapp"
	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/internal/configuration/crypto/domain/documents/crypto_custody_receipt"
	"metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/infrastructure/storage/postgres"
)

type ReceiptIntakeRepo struct {
	*crypto_document_repo.CryptoCustodyReceiptRepo
}

func NewReceiptIntakeRepo() *ReceiptIntakeRepo {
	return &ReceiptIntakeRepo{CryptoCustodyReceiptRepo: crypto_document_repo.NewCryptoCustodyReceiptRepo()}
}

func (r *ReceiptIntakeRepo) LockAdmission(ctx context.Context, basis crypto.AccountingTransferBasis) error {
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return apperror.NewValidation("receipt admission requires a transaction")
	}
	if err := NewCustodyRecognitionRepo().LockScopes(ctx, basis.WalletID); err != nil {
		return err
	}
	return NewCustodyRecognitionRepo().LockIncoming(ctx, basis)
}

// FindByEffect returns the receipt of the logical chain effect the occurrence
// belongs to: a re-included transfer keeps its original receipt.
func (r *ReceiptIntakeRepo) FindByEffect(ctx context.Context, occurrence id.ID) (*crypto_custody_receipt.CryptoCustodyReceipt, error) {
	var docID id.ID
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx, `SELECT d.id FROM doc_crypto_custody_receipts d
		JOIN reg_crypto_accounting_effect_occurrences o ON o.id=d.occurrence_id
		JOIN reg_crypto_accounting_effect_occurrences current ON current.effect_id=o.effect_id AND current.economics_hash=o.economics_hash
		WHERE current.id=$1`, occurrence).Scan(&docID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find custody receipt: %w", err)
	}
	return r.GetByID(ctx, docID)
}

func (r *ReceiptIntakeRepo) AdmissionContext(ctx context.Context, basis crypto.AccountingTransferBasis, evidence crypto.VerifiedCustodyEffect) (cryptoapp.ReceiptAdmissionContext, error) {
	var result cryptoapp.ReceiptAdmissionContext
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	err := q.QueryRow(ctx, `SELECT o.effect_id,w.custody_merchant_id
		FROM reg_crypto_accounting_effect_occurrences o
		JOIN cat_wallets w ON w.id=$2 WHERE o.id=$1`, evidence.OccurrenceID, basis.WalletID).
		Scan(&result.EffectID, &result.CustodyMerchantID)
	if err != nil {
		return result, fmt.Errorf("load receipt custody context: %w", err)
	}
	var blocked bool
	err = q.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM reg_crypto_custody_effect_postings WHERE effect_id=$1)
		OR EXISTS(SELECT 1 FROM reg_crypto_accounting_effect_occurrences o
			JOIN reg_crypto_accounting_effects e ON e.id=o.effect_id
			JOIN cat_wallets w ON w.network_id=e.network_id AND w.address_key=o.source_address
			WHERE o.id=$2)`,
		result.EffectID, evidence.OccurrenceID).Scan(&blocked)
	if err != nil {
		return result, fmt.Errorf("check receipt admission context: %w", err)
	}
	if blocked {
		return result, apperror.NewConflict("receipt effect already has an accounting owner or is an internal transfer")
	}
	return result, nil
}

func (r *ReceiptIntakeRepo) PrepareRecognition(ctx context.Context, doc *crypto_custody_receipt.CryptoCustodyReceipt, _ cryptoapp.ReceiptAdmissionContext) error {
	return NewCustodyRecognitionRepo().ClaimIncoming(ctx,
		crypto.VerifiedCustodyEffect{OccurrenceID: doc.OccurrenceID, VerificationID: doc.VerificationID},
		doc.PostingID, doc.ID, crypto_custody_receipt.DocumentType, doc.WalletID, doc.TokenID, doc.CustodyLineID)
}

func (r *ReceiptIntakeRepo) AppendIntakeAudit(ctx context.Context, doc *crypto_custody_receipt.CryptoCustodyReceipt) error {
	_, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `INSERT INTO reg_crypto_receipt_intake_audit(
		receipt_id,verification_id,posting_id,actor) VALUES($1,$2,$3,'custody-receipt-worker')`,
		doc.ID, doc.VerificationID, doc.PostingID)
	return err
}

func (r *ReceiptIntakeRepo) AnchorAudit(ctx context.Context, receiptID, verificationID id.ID) error {
	if _, err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).Exec(ctx, `UPDATE reg_crypto_custody_receipt_audits
		SET anchor_verification_id=$2 WHERE receipt_id=$1 AND status='canonical'
		  AND COALESCE(anchor_verification_id,(SELECT verification_id FROM doc_crypto_custody_receipts WHERE id=$1))<>$2`,
		receiptID, verificationID); err != nil {
		return fmt.Errorf("anchor receipt canonicality audit: %w", err)
	}
	return nil
}

var _ cryptoapp.ReceiptIntakeRepository = (*ReceiptIntakeRepo)(nil)

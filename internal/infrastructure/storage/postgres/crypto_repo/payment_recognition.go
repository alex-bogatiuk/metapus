package crypto_repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
)

type PaymentRecognitionRepo struct{}

func NewPaymentRecognitionRepo() *PaymentRecognitionRepo { return &PaymentRecognitionRepo{} }

func (*PaymentRecognitionRepo) RequiresRecognition(ctx context.Context, p *crypto_payment.CryptoPayment) (bool, error) {
	var required bool
	err := postgres.MustGetTxManager(ctx).GetQuerier(ctx).QueryRow(ctx,
		"SELECT fn_crypto_payment_requires_recognition($1,$2,$3,$4,$5)", p.ID, p.NetworkID, p.TxHash, p.EventKind, p.EventIndex).Scan(&required)
	return required, err
}

func (*PaymentRecognitionRepo) Authorize(ctx context.Context, p *crypto_payment.CryptoPayment, verification id.ID) (crypto.VerifiedCustodyEffect, error) {
	var empty crypto.VerifiedCustodyEffect
	basis, err := lockPaymentRecognition(ctx, p, verification)
	if err != nil {
		return empty, err
	}
	guard := NewBlockIngestionRepo()
	event, err := guard.ExpectedAccountingTransfer(ctx, basis)
	if err != nil {
		return empty, err
	}
	event.VerificationID = &verification
	event.Confirmations = p.Confirmations
	event.Finality, event.Canonicality = crypto.FinalitySolidified, crypto.CanonicalityCanonical
	if p.Canonicality != "canonical" || (p.FinalityStatus != "solidified" && p.FinalityStatus != "finalized") {
		return empty, crypto.ErrVerificationPending
	}
	return guard.AuthorizeCustodyEffect(ctx, basis, event)
}

func lockPaymentRecognition(ctx context.Context, p *crypto_payment.CryptoPayment, verification id.ID) (crypto.AccountingTransferBasis, error) {
	basis := accountingPaymentBasis(p)
	basis.VerificationID = &verification
	tm := postgres.MustGetTxManager(ctx)
	if tm.GetTx(ctx) == nil {
		return basis, apperror.NewValidation("payment recognition requires a transaction")
	}
	q := tm.GetQuerier(ctx)
	if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoPayment", p.ID); err != nil {
		return basis, err
	}
	if err := NewCustodyRecognitionRepo().LockWallet(ctx, p.WalletID, p.TokenID); err != nil {
		return basis, err
	}
	if _, err := q.Exec(ctx, "SELECT id FROM doc_crypto_payments WHERE id=$1 FOR UPDATE", p.ID); err != nil {
		return basis, err
	}
	if err := NewCustodyRecognitionRepo().LockIncoming(ctx, basis); err != nil {
		return basis, err
	}
	return basis, nil
}

func (*PaymentRecognitionRepo) LoadForAdoption(ctx context.Context, paymentID id.ID) (*crypto_payment.CryptoPayment, error) {
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var network id.ID
	if err := q.QueryRow(ctx, "SELECT network_id FROM doc_crypto_payments WHERE id=$1", paymentID).Scan(&network); err != nil {
		return nil, err
	}
	if err := postgres.NewDocLocker().LockDocument(ctx, "CryptoPayment", paymentID); err != nil {
		return nil, err
	}
	p, err := document_repo.NewCryptoPaymentRepo().GetByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if err := NewCustodyRecognitionRepo().LockScopes(ctx, p.WalletID); err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, "SELECT fn_crypto_receipt_maintenance_guard($1)", network); err != nil {
		return nil, err
	}
	if err := NewCustodyRecognitionRepo().LockWallet(ctx, p.WalletID, p.TokenID); err != nil {
		return nil, err
	}
	return document_repo.NewCryptoPaymentRepo().GetByIDForUpdate(ctx, paymentID)
}

func (*PaymentRecognitionRepo) OriginalMovement(ctx context.Context, p *crypto_payment.CryptoPayment) (entity.CryptoBalanceMovement, error) {
	rows, err := register_repo.NewCryptoBalanceRepo().GetMovementsByRecorder(ctx, p.ID)
	if err != nil {
		return entity.CryptoBalanceMovement{}, err
	}
	var result entity.CryptoBalanceMovement
	count := 0
	for _, m := range rows {
		if m.RecorderType == "CryptoPayment" && m.RecorderVersion == p.PostedVersion {
			result = m
			count++
		}
	}
	if count != 1 || result.RecordType != entity.RecordTypeReceipt || result.WalletID != p.WalletID || result.TokenID != p.TokenID || !result.Amount.Equal(p.Amount) {
		return entity.CryptoBalanceMovement{}, apperror.NewConflict("legacy payment requires exactly one matching original custody line")
	}
	return result, nil
}

func (*PaymentRecognitionRepo) Claim(ctx context.Context, input cryptoapp.PaymentRecognitionInput) (cryptoapp.PaymentRecognitionResult, error) {
	p, m := input.Payment, input.Movement
	result := cryptoapp.PaymentRecognitionResult{PaymentID: p.ID, PostedVersion: m.RecorderVersion}
	q := postgres.MustGetTxManager(ctx).GetQuerier(ctx)
	var occurrence, verification, line id.ID
	var mode, actor, reference string
	err := q.QueryRow(ctx, `SELECT b.posting_id,c.generation,c.occurrence_id,c.verification_id,r.movement_line_id,b.mode,b.actor,b.evidence_reference
		FROM reg_crypto_payment_custody_bindings b JOIN reg_crypto_custody_effect_postings c ON c.id=b.posting_id
		JOIN reg_crypto_custody_effect_recognitions r ON r.posting_id=c.id
		WHERE b.payment_id=$1 AND c.recorder_version=$2`, p.ID, m.RecorderVersion).
		Scan(&result.PostingID, &result.Generation, &occurrence, &verification, &line, &mode, &actor, &reference)
	if err == nil {
		if occurrence != input.Evidence.OccurrenceID || line != m.LineID || mode != input.Mode || actor != input.Actor || reference != input.Reference {
			return result, apperror.NewConflict("payment recognition replay differs from frozen attestation")
		}
		result.Existing = true
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	var effect id.ID
	if err := q.QueryRow(ctx, "SELECT effect_id FROM reg_crypto_accounting_effect_occurrences WHERE id=$1", input.Evidence.OccurrenceID).Scan(&effect); err != nil {
		return result, err
	}
	var predecessor *id.ID
	var owner id.ID
	var ownerType string
	var previousVersion int
	err = q.QueryRow(ctx, `SELECT id,generation,recorder_id,recorder_type,recorder_version FROM reg_crypto_custody_effect_postings
		WHERE effect_id=$1 ORDER BY generation DESC LIMIT 1`, effect).
		Scan(&predecessor, &result.Generation, &owner, &ownerType, &previousVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err == nil && (owner != p.ID || ownerType != "CryptoPayment" || previousVersion+1 != m.RecorderVersion) {
		return result, apperror.NewConflict("custody effect already has another posting owner or version")
	}
	result.Generation++
	result.PostingID = id.New()
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_custody_effect_postings(
		id,effect_id,occurrence_id,verification_id,generation,predecessor_id,recorder_id,recorder_type,recorder_version)
		VALUES($1,$2,$3,$4,$5,$6,$7,'CryptoPayment',$8)`, result.PostingID, effect, input.Evidence.OccurrenceID, input.Evidence.VerificationID,
		result.Generation, predecessor, p.ID, m.RecorderVersion); err != nil {
		return result, fmt.Errorf("claim payment custody owner: %w", err)
	}
	if _, err := q.Exec(ctx, `INSERT INTO reg_crypto_custody_effect_recognitions(posting_id,wallet_id,token_id,record_type,movement_line_id)
		VALUES($1,$2,$3,'receipt',$4)`, result.PostingID, p.WalletID, p.TokenID, m.LineID); err != nil {
		return result, err
	}
	_, err = q.Exec(ctx, `INSERT INTO reg_crypto_payment_custody_bindings(posting_id,payment_id,invoice_id,merchant_id,wallet_id,token_id,
		gross,net,processing_fee,fee_fixed,fee_percent_bp,fee_min,fee_max,period,mode,actor,evidence_reference)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		result.PostingID, p.ID, p.InvoiceID, p.MerchantID, p.WalletID, p.TokenID, p.Amount, p.NetAmount(), p.FeeAmount(), p.FeeFixed, p.FeePercentBP, p.FeeMin, p.FeeMax, p.Date, input.Mode, input.Actor, input.Reference)
	return result, err
}

var _ cryptoapp.PaymentRecognitionRepository = (*PaymentRecognitionRepo)(nil)
